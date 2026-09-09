package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
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
	// Drift delivery retries yield failed rows to later episodes instead of
	// letting the oldest LIMIT page monopolize every reconciliation pass.
	driftAlertRetryBase       = time.Minute
	driftAlertRetryMax        = time.Hour
	driftAlertDispatchTimeout = 30 * time.Second
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
	syncState      domain.SyncStateRepository
	integRepo      domain.IntegrationRepository
	businessRepo   domain.BusinessRepository
	nc             natsRequester
	fetchers       map[string]platform.RemoteFetcher
	tiers          tierResolver
	now            func() time.Time
	tasks          domain.AgentTaskRepository
	driftDMEnabled bool
	publicURL      string
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

// SetDriftAlerts wires the informational task and optional private-DM delivery.
// The environment gate is independent from each business's explicit consent.
func (s *ReconciliationService) SetDriftAlerts(tasks domain.AgentTaskRepository, dmEnabled bool, publicURL string) {
	s.tasks = tasks
	s.driftDMEnabled = dmEnabled
	s.publicURL = strings.TrimRight(publicURL, "/")
}

// Reconcile runs one reconcile pass: it enrolls any newly connected channel,
// then fetches + compares every due channel. It is a sweeperFunc — the returned
// count is the number of channels found drifting this pass. Per-channel fetch
// failures are recorded (backoff) but never fail the pass.
func (s *ReconciliationService) Reconcile(ctx context.Context) (int, error) {
	var count int
	locked, err := s.syncState.WithReconcileLock(ctx, func() error {
		var runErr error
		count, runErr = s.reconcilePass(ctx)
		return runErr
	})
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	return count, nil
}

func (s *ReconciliationService) reconcilePass(ctx context.Context) (int, error) {
	var passErrs []error
	if err := s.enroll(ctx); err != nil {
		passErrs = append(passErrs, err)
	}

	now := s.now()
	due, err := s.syncState.ListDue(ctx, now, reconcileBatchSize)
	if err != nil {
		return 0, err
	}

	var drifted atomic.Int64
	var errsMu sync.Mutex
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
				errsMu.Lock()
				passErrs = append(passErrs, fmt.Errorf("check %s/%s: %w", row.Platform, row.ExternalID, rerr))
				errsMu.Unlock()
				return
			}
			if d {
				drifted.Add(1)
			}
		}(row)
	}
	wg.Wait()
	if err := s.retryPendingAlerts(ctx); err != nil {
		passErrs = append(passErrs, err)
	}
	return int(drifted.Load()), errors.Join(passErrs...)
}

// enroll upserts a sync_state row for every active integration on a supported
// platform so newly connected channels are picked up on the next pass. Errors
// are logged and skipped — enrollment is best-effort and retried each pass.
func (s *ReconciliationService) enroll(ctx context.Context) error {
	integs, err := s.integRepo.ListAllActiveByPlatforms(ctx, reconcileSupportedPlatforms)
	if err != nil {
		slog.ErrorContext(ctx, "reconcile: enroll list integrations failed", "error", err)
		return fmt.Errorf("list integrations for enrollment: %w", err)
	}
	var errs []error
	for _, integ := range integs {
		if err := s.syncState.UpsertPending(ctx, integ.BusinessID, integ.Platform, integ.ExternalID); err != nil {
			slog.ErrorContext(ctx, "reconcile: enroll upsert failed",
				"business_id", integ.BusinessID, "platform", integ.Platform, "error", err)
			errs = append(errs, fmt.Errorf("enroll %s/%s: %w", integ.Platform, integ.ExternalID, err))
		}
	}
	return errors.Join(errs...)
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
	episode, err := s.syncState.MarkCheckedEpisode(ctx, row.ID, snapshot.Fields, drift, now, next)
	if err != nil {
		return false, err
	}
	if drifted {
		if err := s.deliverOrScheduleDriftAlert(ctx, episode); err != nil {
			return drifted, err
		}
	}
	return drifted, nil
}

func (s *ReconciliationService) retryPendingAlerts(ctx context.Context) error {
	pending, err := s.syncState.ListPendingDriftAlerts(ctx)
	if err != nil {
		return fmt.Errorf("list pending drift alerts: %w", err)
	}
	var errs []error
	for _, episode := range pending {
		if err := s.deliverOrScheduleDriftAlert(ctx, episode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *ReconciliationService) deliverOrScheduleDriftAlert(ctx context.Context, episode domain.DriftAlertEpisode) error {
	err := s.deliverDriftAlert(ctx, episode)
	if err == nil {
		return nil
	}
	retryAt := s.now().Add(driftAlertRetryDelay(episode.RetryCount))
	if scheduleErr := s.syncState.ScheduleDriftAlertRetry(ctx, episode.SyncStateID, episode.EpisodeID, retryAt); scheduleErr != nil {
		return errors.Join(err, fmt.Errorf("schedule drift alert retry: %w", scheduleErr))
	}
	return err
}

func driftAlertRetryDelay(retryCount int) time.Duration {
	delay := driftAlertRetryBase
	for i := 0; i < retryCount && delay < driftAlertRetryMax; i++ {
		delay *= 2
	}
	if delay > driftAlertRetryMax {
		return driftAlertRetryMax
	}
	return delay
}

func (s *ReconciliationService) deliverDriftAlert(ctx context.Context, episode domain.DriftAlertEpisode) error {
	if episode.Platform != a2a.AgentVK && episode.Platform != a2a.AgentYandexBusiness {
		return nil
	}
	current, err := s.syncState.GetCurrentDriftAlert(ctx, episode.SyncStateID, episode.EpisodeID)
	if errors.Is(err, domain.ErrDriftEpisodeNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revalidate drift episode: %w", err)
	}
	episode = current
	business, err := s.businessRepo.GetByID(ctx, episode.BusinessID)
	if errors.Is(err, domain.ErrBusinessNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revalidate drift business: %w", err)
	}
	if s.tasks != nil && episode.TaskCreatedAt == nil {
		id := "drift-alert-" + episode.EpisodeID.String()
		href := "/integrations?businessId=" + episode.BusinessID.String()
		input := map[string]interface{}{"fields": episode.Fields, "href": href, "businessId": episode.BusinessID.String()}
		if _, ok, lookupErr := s.privateDriftRecipient(ctx, episode.BusinessID); lookupErr == nil && !ok {
			input["dmPrerequisite"] = "link_private_telegram"
		}
		_, err := s.tasks.GetByID(ctx, episode.BusinessID.String(), id)
		if errors.Is(err, domain.ErrAgentTaskNotFound) {
			err = s.tasks.Create(ctx, &domain.AgentTask{
				ID: id, BusinessID: episode.BusinessID.String(), Type: "drift_alert",
				DisplayName: "Профиль площадки изменился", DisplayNameKey: "sync.drift_alert",
				Status: "done", Platform: episode.Platform,
				Input: input,
			})
		}
		if err == nil {
			if err := s.syncState.MarkDriftTaskCreated(ctx, episode.SyncStateID, episode.EpisodeID); err != nil {
				return fmt.Errorf("acknowledge drift task: %w", err)
			}
		} else {
			return fmt.Errorf("create drift task: %w", err)
		}
	}
	if !s.driftDMEnabled || episode.DMSettledAt != nil || s.nc == nil {
		return s.settleDriftDM(ctx, episode)
	}
	pref := platform.DriftAlertFromSettings(business.Settings)
	if !pref.Enabled {
		return s.settleDriftDM(ctx, episode)
	}
	chatID, ok, err := s.privateDriftRecipient(ctx, episode.BusinessID)
	if err != nil {
		return fmt.Errorf("resolve private drift recipient: %w", err)
	}
	if !ok {
		return s.settleDriftDM(ctx, episode)
	}
	fields := localizedDriftFields(episode.Fields, pref.Locale)
	href := s.publicURL + "/integrations?businessId=" + episode.BusinessID.String()
	text := "Профиль организации изменился на площадке " + driftPlatformName(episode.Platform, "ru") + ". Поля: " + fields + ". Проверьте организацию: " + href
	if pref.Locale == "en" {
		text = "An organization profile changed on " + driftPlatformName(episode.Platform, "en") + ". Fields: " + fields + ". Review this organization: " + href
	}
	args := map[string]interface{}{"text": text, "chat_id": chatID}
	approvalID := "drift-alert-" + episode.EpisodeID.String()
	if _, err := dispatchToolWithApproval(ctx, s.nc, a2a.AgentTelegram, tools.TelegramSendNotification,
		args, episode.BusinessID.String(), approvalID, driftAlertDispatchTimeout); err != nil {
		return fmt.Errorf("deliver drift DM for %s: %w", episode.BusinessID, err)
	}
	if err := s.syncState.MarkDriftDMSettled(ctx, episode.SyncStateID, episode.EpisodeID); err != nil {
		return fmt.Errorf("acknowledge drift DM: %w", err)
	}
	return nil
}

func (s *ReconciliationService) settleDriftDM(ctx context.Context, episode domain.DriftAlertEpisode) error {
	if episode.DMSettledAt != nil {
		return nil
	}
	return s.syncState.MarkDriftDMSettled(ctx, episode.SyncStateID, episode.EpisodeID)
}

func (s *ReconciliationService) privateDriftRecipient(ctx context.Context, businessID uuid.UUID) (chatID string, found bool, err error) {
	integs, err := s.integRepo.ListByBusinessAndPlatform(ctx, businessID, a2a.AgentTelegram)
	if err != nil {
		return "", false, err
	}
	for _, integ := range integs {
		if integ.Status != domain.IntegrationStatusActive {
			continue
		}
		if id, ok := integ.Metadata["telegram_user_id"].(string); ok {
			value := strings.TrimSpace(id)
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err == nil && parsed > 0 {
				return value, true, nil
			}
		}
	}
	return "", false, nil
}

func localizedDriftFields(fields []string, locale string) string {
	labels := map[string][2]string{
		platform.FieldTitle:       {"название", "name"},
		platform.FieldDescription: {"описание", "description"},
		platform.FieldWebsite:     {"сайт", "website"},
		platform.FieldSchedule:    {"часы работы", "business hours"},
	}
	idx := 0
	if locale == "en" {
		idx = 1
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if label, ok := labels[field]; ok {
			out = append(out, label[idx])
		}
	}
	return strings.Join(out, ", ")
}

func driftPlatformName(platformID, locale string) string {
	if platformID == a2a.AgentVK {
		return "VK"
	}
	if locale == "en" {
		return "Yandex Business"
	}
	return "Яндекс Бизнес"
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
