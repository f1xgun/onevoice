package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// Reconciler cadence + batch tunables.
const (
	// reconcileBaseCadence is the default (Free-tier) re-check interval and the
	// backoff base. Yandex is always held at this cadence regardless of tier
	// because each Yandex check spins up an RPA browser page.
	reconcileBaseCadence = 24 * time.Hour
	// reconcilePaidCadence is the re-check interval for any non-Free tier
	// (Telegram/VK only — Yandex stays at the base cadence).
	reconcilePaidCadence = 6 * time.Hour
	// reconcileMaxBackoff caps the exponential failure backoff.
	reconcileMaxBackoff = 7 * 24 * time.Hour
	// reconcileBatchSize bounds how many due rows one pass claims.
	reconcileBatchSize = 100
	// reconcileConcurrency bounds concurrent fetches so a fleet-wide pass never
	// bursts the platform agents (the Yandex RPA agent especially).
	reconcileConcurrency = 4
	// reconcileFetchTimeout bounds a single remote fetch. Generous because the
	// Yandex path is an RPA browser round-trip.
	reconcileFetchTimeout = 90 * time.Second
)

// reconcileSupportedPlatforms are the platforms whose profile OneVoice writes
// and can therefore reconcile. Google is excluded (it writes nothing).
var reconcileSupportedPlatforms = []string{
	a2a.AgentTelegram,
	a2a.AgentVK,
	a2a.AgentYandexBusiness,
}

// tierResolver resolves a business's effective plan so the reconciler can pick
// its re-check cadence. *planresolver.Resolver satisfies it; tests inject a
// resolver over a fake Store.
type tierResolver interface {
	Resolve(ctx context.Context, businessID uuid.UUID) planresolver.Plan
}

// ReconciliationService periodically compares each connected channel's live
// profile against the stored business profile and records any drift for manual
// repair. It never auto-heals: on drift it only stores + exposes the delta; the
// verify endpoint re-invokes the existing SyncBusiness re-push.
type ReconciliationService struct {
	syncState    domain.SyncStateRepository
	integRepo    domain.IntegrationRepository
	businessRepo domain.BusinessRepository
	nc           natsRequester
	fetchers     map[string]platform.RemoteFetcher
	tiers        tierResolver
	now          func() time.Time
}

// NewReconciliationService wires the reconciler. nc may be nil (Yandex checks
// are then skipped as unavailable). fetchers maps the direct-API platforms
// (Telegram, VK) to their RemoteFetcher; Yandex is fetched over NATS.
func NewReconciliationService(
	syncState domain.SyncStateRepository,
	integRepo domain.IntegrationRepository,
	businessRepo domain.BusinessRepository,
	nc *natslib.Conn,
	fetchers map[string]platform.RemoteFetcher,
	tiers tierResolver,
) *ReconciliationService {
	var requester natsRequester
	if nc != nil {
		requester = nc
	}
	return &ReconciliationService{
		syncState:    syncState,
		integRepo:    integRepo,
		businessRepo: businessRepo,
		nc:           requester,
		fetchers:     fetchers,
		tiers:        tiers,
		now:          time.Now,
	}
}

// Reconcile runs one reconcile pass: it enrolls any newly connected channel,
// then fetches + compares every due channel. It is a sweeperFunc — the returned
// count is the number of channels found drifting this pass. Per-channel fetch
// failures are recorded (backoff) but never fail the pass.
func (s *ReconciliationService) Reconcile(ctx context.Context) (int, error) {
	s.enroll(ctx)

	now := s.now()
	due, err := s.syncState.ListDue(ctx, now, reconcileBatchSize)
	if err != nil {
		return 0, err
	}

	var drifted atomic.Int64
	sem := make(chan struct{}, reconcileConcurrency)
	var wg sync.WaitGroup
	for _, row := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(row domain.SyncState) {
			defer wg.Done()
			defer func() { <-sem }()
			d, rerr := s.reconcileOne(ctx, row)
			if rerr != nil {
				slog.ErrorContext(ctx, "reconcile: channel check failed",
					"business_id", row.BusinessID, "platform", row.Platform, "error", rerr)
				return
			}
			if d {
				drifted.Add(1)
			}
		}(row)
	}
	wg.Wait()
	return int(drifted.Load()), nil
}

// enroll upserts a sync_state row for every active integration on a supported
// platform so newly connected channels are picked up on the next pass. Errors
// are logged and skipped — enrollment is best-effort and retried each pass.
func (s *ReconciliationService) enroll(ctx context.Context) {
	integs, err := s.integRepo.ListAllActiveByPlatforms(ctx, reconcileSupportedPlatforms)
	if err != nil {
		slog.ErrorContext(ctx, "reconcile: enroll list integrations failed", "error", err)
		return
	}
	for _, integ := range integs {
		if err := s.syncState.UpsertPending(ctx, integ.BusinessID, integ.Platform, integ.ExternalID); err != nil {
			slog.ErrorContext(ctx, "reconcile: enroll upsert failed",
				"business_id", integ.BusinessID, "platform", integ.Platform, "error", err)
		}
	}
}

// reconcileOne fetches, compares, and records the result for a single channel.
// It returns whether the channel is drifting. A fetch failure is not an error:
// it is recorded as a backoff (drift state untouched) and returns (false, nil).
// A non-nil error is reserved for repository failures the caller should log.
func (s *ReconciliationService) reconcileOne(ctx context.Context, row domain.SyncState) (bool, error) {
	now := s.now()

	business, err := s.businessRepo.GetByID(ctx, row.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			// Soft-deleted / gone business: stop polling it, no drift.
			return false, s.syncState.MarkChecked(ctx, row.ID, nil, nil, now, now.Add(reconcileBaseCadence))
		}
		return false, err
	}

	integ, err := s.integRepo.GetByBusinessPlatformExternal(ctx, row.BusinessID, row.Platform, row.ExternalID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return false, s.syncState.MarkChecked(ctx, row.ID, nil, nil, now, now.Add(reconcileBaseCadence))
		}
		return false, err
	}

	start := s.now()
	snapshot, ferr := s.fetchRemote(ctx, business, *integ)
	metrics.ObserveReconcileFetch(row.Platform, time.Since(start))

	if ferr != nil {
		return false, s.recordFailure(ctx, row, redactPII(ferr.Error()), now, false)
	}
	if snapshot.Err != "" {
		return false, s.recordFailure(ctx, row, redactPII(snapshot.Err), now, looksLikeAuthError(snapshot.Err))
	}

	stored := platform.SyncedSnapshot(business, row.Platform)
	drift := computeDrift(row.Platform, stored, snapshot.Fields)
	drifted := len(drift) > 0

	metrics.SetSyncDrift(row.Platform, drifted)
	if drifted {
		metrics.IncReconcileCheck(row.Platform, metrics.ReconcileResultDrift)
	} else {
		metrics.IncReconcileCheck(row.Platform, metrics.ReconcileResultOK)
	}

	next := now.Add(s.cadence(ctx, business.ID, row.Platform))
	if err := s.syncState.MarkChecked(ctx, row.ID, snapshot.Fields, drift, now, next); err != nil {
		return false, err
	}
	return drifted, nil
}

// recordFailure applies the failure backoff + metric for a fetch that did not
// yield a usable snapshot. authInvalid forces the maximum backoff — a revoked
// token is only cleared by the write path's MarkTokenExpired (which drops the
// row from ListDue's active JOIN), so retrying sooner just wastes quota.
func (s *ReconciliationService) recordFailure(ctx context.Context, row domain.SyncState, reason string, now time.Time, authInvalid bool) error {
	metrics.IncReconcileCheck(row.Platform, metrics.ReconcileResultError)
	back := s.backoff(row.ConsecutiveFailures)
	if authInvalid {
		back = reconcileMaxBackoff
	}
	return s.syncState.MarkFailure(ctx, row.ID, reason, now, now.Add(back))
}

// fetchRemote reads the platform's live profile. Telegram/VK go through their
// direct RemoteFetcher; Yandex is dispatched to its RPA agent over NATS (the
// agent owns the browser session), reading back only the schedule field.
func (s *ReconciliationService) fetchRemote(ctx context.Context, b *domain.Business, integ domain.Integration) (platform.RemoteSnapshot, error) {
	if integ.Platform == a2a.AgentYandexBusiness {
		return s.fetchYandex(ctx, b)
	}
	f, ok := s.fetchers[integ.Platform]
	if !ok {
		return platform.RemoteSnapshot{}, errors.New("reconcile: no fetcher for platform " + integ.Platform)
	}
	return f.FetchRemote(ctx, b, integ)
}

// fetchYandex dispatches the existing yandex_business__get_info tool over NATS
// and maps the RPA agent's reply into a schedule-only snapshot (the only field
// OneVoice writes to Yandex). No agent-side change is required.
func (s *ReconciliationService) fetchYandex(ctx context.Context, b *domain.Business) (platform.RemoteSnapshot, error) {
	if s.nc == nil {
		return platform.RemoteSnapshot{}, errors.New("reconcile: nats unavailable for yandex fetch")
	}
	resp, err := dispatchTool(ctx, s.nc, a2a.AgentYandexBusiness, tools.YandexBusinessGetInfo,
		map[string]interface{}{}, b.ID.String(), reconcileFetchTimeout)
	if err != nil {
		return platform.RemoteSnapshot{}, err
	}
	hours, _ := resp.Result["hours"].(string)
	return platform.RemoteSnapshot{Fields: map[string]string{platform.FieldSchedule: hours}}, nil
}

// cadence picks the re-check interval: Yandex is always the base cadence (RPA is
// expensive); other platforms use the paid cadence for any non-Free tier. The
// plan resolver is fail-safe to Free, so an error biases toward the LONGER
// (base) cadence — the correct, load-shedding direction.
func (s *ReconciliationService) cadence(ctx context.Context, businessID uuid.UUID, platformID string) time.Duration {
	if platformID == a2a.AgentYandexBusiness {
		return reconcileBaseCadence
	}
	if s.tiers == nil {
		return reconcileBaseCadence
	}
	plan := s.tiers.Resolve(ctx, businessID)
	if plan.RateLimitTier != "" && plan.RateLimitTier != "free" {
		return reconcilePaidCadence
	}
	return reconcileBaseCadence
}

// backoff returns the exponential backoff for a fetch that has already failed
// `failures` times, doubling from the base cadence and capped at
// reconcileMaxBackoff. The loop form avoids the shift overflow a `base<<n` would
// hit for large failure counts.
func (s *ReconciliationService) backoff(failures int) time.Duration {
	d := reconcileBaseCadence
	for i := 0; i < failures && d < reconcileMaxBackoff; i++ {
		d *= 2
	}
	if d > reconcileMaxBackoff {
		d = reconcileMaxBackoff
	}
	return d
}

// ListDrift returns the current per-channel sync/drift state for a business,
// backing the GET …/integrations/drift endpoint.
func (s *ReconciliationService) ListDrift(ctx context.Context, businessID uuid.UUID) ([]domain.SyncState, error) {
	return s.syncState.ListByBusinessID(ctx, businessID)
}

// ScheduleImmediate marks every channel of a business due right now so the next
// pass re-checks it. Called by the verify-and-repair endpoint after it re-pushes
// the profile.
func (s *ReconciliationService) ScheduleImmediate(ctx context.Context, businessID uuid.UUID) error {
	return s.syncState.ScheduleImmediate(ctx, businessID)
}

// looksLikeAuthError heuristically classifies a platform error message as a
// token/permission problem, so the reconciler applies the maximum backoff
// instead of hammering a revoked channel.
func looksLikeAuthError(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "token") ||
		strings.Contains(m, "unauthor") ||
		strings.Contains(m, "permission") ||
		strings.Contains(m, "access denied") ||
		strings.Contains(m, "auth")
}

// piiDigitRun matches a run of 7+ digits so a phone number that leaks into an
// error string is masked before it is persisted to last_error or logged.
var piiDigitRun = regexp.MustCompile(`\d{7,}`)

// redactPII masks long digit runs (phone-shaped) from an error string.
func redactPII(s string) string {
	return piiDigitRun.ReplaceAllString(s, "[REDACTED]")
}
