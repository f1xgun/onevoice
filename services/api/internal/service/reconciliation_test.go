package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// --- fakes ---------------------------------------------------------------

type markCheckedCall struct {
	id          uuid.UUID
	driftFields []string
	nextCheckAt time.Time
}

type markFailureCall struct {
	id          uuid.UUID
	lastError   string
	nextCheckAt time.Time
}

type fakeSyncStateRepo struct {
	domain.SyncStateRepository
	mu       sync.Mutex
	due      []domain.SyncState
	upserts  int
	checked  []markCheckedCall
	failures []markFailureCall
}

func (f *fakeSyncStateRepo) UpsertPending(_ context.Context, _ uuid.UUID, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts++
	return nil
}

func (f *fakeSyncStateRepo) ListDue(_ context.Context, _ time.Time, _ int) ([]domain.SyncState, error) {
	return f.due, nil
}

func (f *fakeSyncStateRepo) MarkChecked(_ context.Context, id uuid.UUID, _ map[string]string, driftFields []string, _, nextCheckAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked = append(f.checked, markCheckedCall{id: id, driftFields: driftFields, nextCheckAt: nextCheckAt})
	return nil
}

func (f *fakeSyncStateRepo) MarkFailure(_ context.Context, id uuid.UUID, lastError string, _, nextCheckAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, markFailureCall{id: id, lastError: lastError, nextCheckAt: nextCheckAt})
	return nil
}

type fakeReconcileIntegRepo struct {
	domain.IntegrationRepository
	active []domain.Integration
	byKey  map[string]*domain.Integration
}

func (f *fakeReconcileIntegRepo) ListAllActiveByPlatforms(_ context.Context, _ []string) ([]domain.Integration, error) {
	return f.active, nil
}

func (f *fakeReconcileIntegRepo) GetByBusinessPlatformExternal(_ context.Context, _ uuid.UUID, plat, ext string) (*domain.Integration, error) {
	if integ, ok := f.byKey[plat+"|"+ext]; ok {
		return integ, nil
	}
	return nil, domain.ErrIntegrationNotFound
}

type fakeReconcileBizRepo struct {
	domain.BusinessRepository
	biz *domain.Business
	err error
}

func (f *fakeReconcileBizRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.biz, nil
}

type fakeFetcher struct {
	snap platform.RemoteSnapshot
	err  error
}

func (f fakeFetcher) FetchRemote(_ context.Context, _ *domain.Business, _ domain.Integration) (platform.RemoteSnapshot, error) {
	return f.snap, f.err
}

// stubYandexInfoRequester replies to every NATS request with a success
// get_info ToolResponse carrying a rendered hours string (copy of the
// stubReviewsRequester pattern).
type stubYandexInfoRequester struct{ hours string }

func (s stubYandexInfoRequester) RequestMsgWithContext(_ context.Context, _ *natslib.Msg) (*natslib.Msg, error) {
	resp, _ := json.Marshal(a2a.ToolResponse{
		Success: true,
		Result:  map[string]interface{}{"hours": s.hours},
	})
	return &natslib.Msg{Data: resp}, nil
}

type fakePlanStore struct {
	plan planresolver.Plan
	err  error
}

func (f fakePlanStore) ActivePlanForBusiness(_ context.Context, _ uuid.UUID) (planresolver.Plan, error) {
	return f.plan, f.err
}

func (f fakePlanStore) FreePlan(_ context.Context) (planresolver.Plan, error) {
	return planresolver.Plan{Code: "free", RateLimitTier: "free"}, nil
}

// --- helpers -------------------------------------------------------------

func telegramRow(bizID uuid.UUID, failures int) domain.SyncState {
	return domain.SyncState{
		ID:                  uuid.New(),
		BusinessID:          bizID,
		Platform:            a2a.AgentTelegram,
		ExternalID:          "@chan",
		ConsecutiveFailures: failures,
	}
}

func newReconcilerFor(ss domain.SyncStateRepository, integ domain.IntegrationRepository, biz domain.BusinessRepository, fetchers map[string]platform.RemoteFetcher, tiers tierResolver, now time.Time) *ReconciliationService {
	return &ReconciliationService{
		syncState:    ss,
		integRepo:    integ,
		businessRepo: biz,
		fetchers:     fetchers,
		tiers:        tiers,
		now:          func() time.Time { return now },
	}
}

// --- tests ---------------------------------------------------------------

// TestReconcileOne_DriftDetected is the reconciler-level fail-on-revert: with
// the stored name "New" and the platform returning the OLD name "Old", one pass
// must record drift on [title], flip the drift metric, and reschedule (not fail).
//
// Fail-on-revert: revert computeDrift to a no-op and MarkChecked receives an
// empty drift set + the drift counter never increments — both assertions fail.
func TestReconcileOne_DriftDetected(t *testing.T) {
	bizID := uuid.New()
	biz := &domain.Business{ID: bizID, Name: "New", Description: "same desc"}
	integ := &domain.Integration{BusinessID: bizID, Platform: a2a.AgentTelegram, ExternalID: "@chan", Status: domain.IntegrationStatusActive}

	ss := &fakeSyncStateRepo{}
	fetcher := fakeFetcher{snap: platform.RemoteSnapshot{Fields: map[string]string{
		platform.FieldTitle:       "Old",
		platform.FieldDescription: "same desc",
	}}}
	svc := newReconcilerFor(ss,
		&fakeReconcileIntegRepo{byKey: map[string]*domain.Integration{a2a.AgentTelegram + "|@chan": integ}},
		&fakeReconcileBizRepo{biz: biz},
		map[string]platform.RemoteFetcher{a2a.AgentTelegram: fetcher},
		nil, time.Now())

	before := testutil.ToFloat64(metrics.GetReconcileCheckCounter().WithLabelValues(a2a.AgentTelegram, metrics.ReconcileResultDrift))

	drifted, err := svc.reconcileOne(context.Background(), telegramRow(bizID, 0))
	if err != nil {
		t.Fatalf("reconcileOne error: %v", err)
	}
	if !drifted {
		t.Fatal("expected drift to be detected")
	}
	if len(ss.checked) != 1 || len(ss.checked[0].driftFields) != 1 || ss.checked[0].driftFields[0] != platform.FieldTitle {
		t.Fatalf("expected MarkChecked with driftFields=[title], got %+v", ss.checked)
	}
	after := testutil.ToFloat64(metrics.GetReconcileCheckCounter().WithLabelValues(a2a.AgentTelegram, metrics.ReconcileResultDrift))
	if after <= before {
		t.Fatalf("drift metric must increment (before=%v after=%v)", before, after)
	}
}

// TestReconcileOne_FetchErrorBacksOff proves a transport failure records a
// backoff (no drift mutation) rather than surfacing as drift or an error.
func TestReconcileOne_FetchErrorBacksOff(t *testing.T) {
	bizID := uuid.New()
	biz := &domain.Business{ID: bizID, Name: "Shop"}
	integ := &domain.Integration{BusinessID: bizID, Platform: a2a.AgentTelegram, ExternalID: "@chan", Status: domain.IntegrationStatusActive}
	now := time.Now()

	ss := &fakeSyncStateRepo{}
	svc := newReconcilerFor(ss,
		&fakeReconcileIntegRepo{byKey: map[string]*domain.Integration{a2a.AgentTelegram + "|@chan": integ}},
		&fakeReconcileBizRepo{biz: biz},
		map[string]platform.RemoteFetcher{a2a.AgentTelegram: fakeFetcher{err: errors.New("dial tcp: timeout")}},
		nil, now)

	drifted, err := svc.reconcileOne(context.Background(), telegramRow(bizID, 1))
	if err != nil || drifted {
		t.Fatalf("fetch error must be a benign backoff, got drifted=%v err=%v", drifted, err)
	}
	if len(ss.checked) != 0 {
		t.Fatalf("drift state must not be touched on fetch error, got %+v", ss.checked)
	}
	if len(ss.failures) != 1 {
		t.Fatalf("expected one MarkFailure, got %+v", ss.failures)
	}
	// ConsecutiveFailures=1 → backoff = base<<1 = 48h.
	wantNext := now.Add(48 * time.Hour)
	if !ss.failures[0].nextCheckAt.Equal(wantNext) {
		t.Fatalf("expected next_check_at=%v (48h backoff), got %v", wantNext, ss.failures[0].nextCheckAt)
	}
}

// TestReconcileOne_AuthErrorMaxBackoff proves an auth/token error envelope is
// held at the maximum backoff (the write path clears it, not the reconciler).
func TestReconcileOne_AuthErrorMaxBackoff(t *testing.T) {
	bizID := uuid.New()
	biz := &domain.Business{ID: bizID, Name: "Shop"}
	integ := &domain.Integration{BusinessID: bizID, Platform: a2a.AgentVK, ExternalID: "g1", Status: domain.IntegrationStatusActive}
	now := time.Now()

	ss := &fakeSyncStateRepo{}
	svc := newReconcilerFor(ss,
		&fakeReconcileIntegRepo{byKey: map[string]*domain.Integration{a2a.AgentVK + "|g1": integ}},
		&fakeReconcileBizRepo{biz: biz},
		map[string]platform.RemoteFetcher{a2a.AgentVK: fakeFetcher{snap: platform.RemoteSnapshot{Err: "User authorization failed: access_token has expired."}}},
		nil, now)

	if _, err := svc.reconcileOne(context.Background(), domain.SyncState{ID: uuid.New(), BusinessID: bizID, Platform: a2a.AgentVK, ExternalID: "g1"}); err != nil {
		t.Fatalf("reconcileOne error: %v", err)
	}
	if len(ss.failures) != 1 {
		t.Fatalf("expected one MarkFailure, got %+v", ss.failures)
	}
	wantNext := now.Add(reconcileMaxBackoff)
	if !ss.failures[0].nextCheckAt.Equal(wantNext) {
		t.Fatalf("auth error must use max backoff %v, got %v", wantNext, ss.failures[0].nextCheckAt)
	}
}

// TestReconcile_DueSelection proves every due row is enrolled + processed in one
// pass and the drift count is returned.
func TestReconcile_DueSelection(t *testing.T) {
	bizA, bizB := uuid.New(), uuid.New()
	integA := &domain.Integration{BusinessID: bizA, Platform: a2a.AgentTelegram, ExternalID: "@chan", Status: domain.IntegrationStatusActive}
	integB := &domain.Integration{BusinessID: bizB, Platform: a2a.AgentTelegram, ExternalID: "@chan", Status: domain.IntegrationStatusActive}

	ss := &fakeSyncStateRepo{due: []domain.SyncState{telegramRow(bizA, 0), telegramRow(bizB, 0)}}
	integRepo := &fakeReconcileIntegRepo{
		active: []domain.Integration{*integA, *integB},
		byKey: map[string]*domain.Integration{
			a2a.AgentTelegram + "|@chan": integA, // both rows share the external id key
		},
	}
	// Both businesses resolve to the same fake biz; the fetcher reports in-sync.
	svc := newReconcilerFor(ss, integRepo,
		&fakeReconcileBizRepo{biz: &domain.Business{ID: bizA, Name: "Shop"}},
		map[string]platform.RemoteFetcher{a2a.AgentTelegram: fakeFetcher{snap: platform.RemoteSnapshot{Fields: map[string]string{platform.FieldTitle: "Shop", platform.FieldDescription: ""}}}},
		nil, time.Now())

	n, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 drifting channels, got %d", n)
	}
	if ss.upserts != 2 {
		t.Fatalf("expected 2 enroll upserts, got %d", ss.upserts)
	}
	if len(ss.checked) != 2 {
		t.Fatalf("expected both due rows checked, got %d", len(ss.checked))
	}
}

// TestCadenceByTier pins the per-tier cadence: Free=24h, paid=6h, and Yandex is
// always 24h regardless of tier (RPA is expensive).
func TestCadenceByTier(t *testing.T) {
	bizID := uuid.New()
	ctx := context.Background()

	freeResolver := planresolver.New(fakePlanStore{err: domain.ErrSubscriptionNotFound}, time.Minute)
	proResolver := planresolver.New(fakePlanStore{plan: planresolver.Plan{Code: "pro", RateLimitTier: "pro"}}, time.Minute)

	svcFree := &ReconciliationService{tiers: freeResolver}
	if got := svcFree.cadence(ctx, bizID, a2a.AgentTelegram); got != reconcileBaseCadence {
		t.Errorf("free tier Telegram cadence = %v, want %v", got, reconcileBaseCadence)
	}

	svcPro := &ReconciliationService{tiers: proResolver}
	if got := svcPro.cadence(ctx, bizID, a2a.AgentTelegram); got != reconcilePaidCadence {
		t.Errorf("pro tier Telegram cadence = %v, want %v", got, reconcilePaidCadence)
	}
	if got := svcPro.cadence(ctx, bizID, a2a.AgentYandexBusiness); got != reconcileBaseCadence {
		t.Errorf("Yandex cadence must always be base %v even on a paid tier, got %v", reconcileBaseCadence, got)
	}
}

// TestBackoff pins the exponential backoff schedule and its 7d cap.
func TestBackoff(t *testing.T) {
	svc := &ReconciliationService{}
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, 24 * time.Hour},
		{1, 48 * time.Hour},
		{2, 96 * time.Hour},
		{3, 7 * 24 * time.Hour},  // 192h capped to 168h
		{10, 7 * 24 * time.Hour}, // cap holds, no overflow
	}
	for _, tc := range cases {
		if got := svc.backoff(tc.failures); got != tc.want {
			t.Errorf("backoff(%d) = %v, want %v", tc.failures, got, tc.want)
		}
	}
}

// TestYandexFetch_ViaNATSStub proves the Yandex path dispatches get_info over
// NATS and compares only the schedule field — a shared hour boundary is no drift.
func TestYandexFetch_ViaNATSStub(t *testing.T) {
	bizID := uuid.New()
	biz := &domain.Business{
		ID:   bizID,
		Name: "Shop",
		Settings: map[string]interface{}{
			"schedule": []map[string]interface{}{
				{"day": "mon", "open": "09:00", "close": "21:00"},
			},
		},
	}
	integ := &domain.Integration{BusinessID: bizID, Platform: a2a.AgentYandexBusiness, ExternalID: "perm-1", Status: domain.IntegrationStatusActive}

	ss := &fakeSyncStateRepo{}
	svc := &ReconciliationService{
		syncState:    ss,
		integRepo:    &fakeReconcileIntegRepo{byKey: map[string]*domain.Integration{a2a.AgentYandexBusiness + "|perm-1": integ}},
		businessRepo: &fakeReconcileBizRepo{biz: biz},
		nc:           stubYandexInfoRequester{hours: "Пн-Пт 09:00–21:00"},
		now:          time.Now,
	}

	drifted, err := svc.reconcileOne(context.Background(), domain.SyncState{ID: uuid.New(), BusinessID: bizID, Platform: a2a.AgentYandexBusiness, ExternalID: "perm-1"})
	if err != nil {
		t.Fatalf("reconcileOne error: %v", err)
	}
	if drifted {
		t.Fatal("shared hour boundary must not be flagged as drift")
	}
	if len(ss.checked) != 1 {
		t.Fatalf("expected one MarkChecked, got %+v", ss.checked)
	}
}
