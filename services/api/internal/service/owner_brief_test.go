package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// --- fakes -------------------------------------------------------------------

// fakeBriefIntegRepo returns a fixed integration list from
// ListAllActiveByPlatforms; every other IntegrationRepository method panics via
// the embedded nil interface so an unexpected dependency surfaces.
type fakeBriefIntegRepo struct {
	domain.IntegrationRepository
	integrations []domain.Integration
	err          error
}

func (f *fakeBriefIntegRepo) ListAllActiveByPlatforms(_ context.Context, _ []string) ([]domain.Integration, error) {
	return f.integrations, f.err
}

// fakeBriefBusinessRepo is an in-memory BusinessRepository keyed by id. It
// records UpdateSettingsKeys writes so the idempotency stamp can be asserted and
// fed back into the next GetByID (mirroring a persisted settings blob).
type fakeBriefBusinessRepo struct {
	domain.BusinessRepository
	mu         sync.Mutex
	businesses map[uuid.UUID]*domain.Business
	updates    int
}

func (f *fakeBriefBusinessRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Business, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	biz, ok := f.businesses[id]
	if !ok {
		return nil, domain.ErrBusinessNotFound
	}
	clone := *biz
	clone.Settings = cloneSettings(biz.Settings)
	return &clone, nil
}

func (f *fakeBriefBusinessRepo) UpdateSettingsKeys(_ context.Context, id uuid.UUID, keys map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	biz, ok := f.businesses[id]
	if !ok {
		return domain.ErrBusinessNotFound
	}
	if biz.Settings == nil {
		biz.Settings = map[string]interface{}{}
	}
	for k, v := range keys {
		biz.Settings[k] = v
	}
	return nil
}

func cloneSettings(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// fakeBriefStats returns a fixed aggregate for any business.
type fakeBriefStats struct {
	stats OwnerBriefStats
	err   error
}

func (f fakeBriefStats) FetchStats(_ context.Context, _ string, _ time.Time) (OwnerBriefStats, error) {
	return f.stats, f.err
}

// recordingRouter records every ChatRequest and returns a canned reply (or a
// canned error, to exercise the templated fallback).
type recordingRouter struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	reply    string
	err      error
}

func (r *recordingRouter) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	return &llm.ChatResponse{Content: r.reply}, nil
}

func (r *recordingRouter) lastPromptText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, req := range r.requests {
		for _, m := range req.Messages {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// recordingRequester records each dispatched NATS message and replies with a
// success ToolResponse so the send is treated as confirmed.
type recordingRequester struct {
	mu    sync.Mutex
	sent  []dispatchedMsg
	reply *a2a.ToolResponse
}

type dispatchedMsg struct {
	subject string
	args    map[string]interface{}
	tool    string
}

func (r *recordingRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	var req a2a.ToolRequest
	_ = json.Unmarshal(msg.Data, &req)
	r.mu.Lock()
	r.sent = append(r.sent, dispatchedMsg{subject: msg.Subject, args: req.Args, tool: req.Tool})
	r.mu.Unlock()
	resp := r.reply
	if resp == nil {
		resp = &a2a.ToolResponse{Success: true, Result: map[string]interface{}{"status": "sent"}}
	}
	data, _ := json.Marshal(resp)
	return &natslib.Msg{Data: data}, nil
}

func (r *recordingRequester) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// recordingTelemetry captures Ingest calls.
type recordingTelemetry struct {
	mu     sync.Mutex
	events []TelemetryEvent
}

func (r *recordingTelemetry) Ingest(_ context.Context, _ uuid.UUID, events []TelemetryEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
	return nil
}

// --- fixtures ----------------------------------------------------------------

// mondayNineAM is a deterministic instant inside the default Monday-09:00 window.
func mondayNineAM() time.Time {
	// 2026-07-06 is a Monday.
	return time.Date(2026, 7, 6, 9, 30, 0, 0, time.UTC)
}

func telegramInteg(bizID uuid.UUID, meta map[string]interface{}) domain.Integration {
	return domain.Integration{
		BusinessID: bizID,
		Platform:   a2a.AgentTelegram,
		Status:     domain.IntegrationStatusActive,
		ExternalID: "-100999",
		Metadata:   meta,
	}
}

func ownerMeta() map[string]interface{} {
	return map[string]interface{}{"telegram_user_id": "42"}
}

// newBriefFixture wires an OwnerBriefService with one enabled, owner-configured
// business and the supplied clock, router, and stats.
func newBriefFixture(t *testing.T, clock time.Time, router OwnerBriefRouter, stats ownerBriefStatsFetcher) (*OwnerBriefService, *fakeBriefBusinessRepo, *recordingRequester, *recordingTelemetry, uuid.UUID) {
	t.Helper()
	bizID := uuid.New()
	bizRepo := &fakeBriefBusinessRepo{
		businesses: map[uuid.UUID]*domain.Business{
			bizID: {ID: bizID, Name: "Кофейня У Дома"},
		},
	}
	integRepo := &fakeBriefIntegRepo{integrations: []domain.Integration{telegramInteg(bizID, ownerMeta())}}
	nc := &recordingRequester{}
	tel := &recordingTelemetry{}
	svc := NewOwnerBriefService(integRepo, bizRepo, stats, router, "test-model", nc, tel)
	svc.now = func() time.Time { return clock }
	return svc, bizRepo, nc, tel, bizID
}

func someStats() OwnerBriefStats {
	return OwnerBriefStats{
		Total: 10, Answered: 7, Unanswered: 3, ReplyRate: 0.7, AverageRating: 4.5,
		RatingDistribution: map[int]int{1: 0, 2: 1, 3: 1, 4: 3, 5: 5},
		RecentDays:         7, RecentTotal: 4, RecentAnswered: 2,
	}
}

// --- tests -------------------------------------------------------------------

// TestOwnerBrief_WeeklyIdempotency is the fail-on-revert guard for the core
// guarantee: two RunOnce passes in the same ISO week send EXACTLY ONE brief.
//
// Fail-on-revert: remove the stampSent write (or the lastSent==isoWeek skip in
// processBusiness) and the second pass dispatches a second DM — the "want 1"
// assertion fails.
func TestOwnerBrief_WeeklyIdempotency(t *testing.T) {
	router := &recordingRouter{reply: "Ваша недельная сводка."}
	svc, bizRepo, nc, _, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	if got := nc.count(); got != 1 {
		t.Fatalf("dispatches across two same-week passes = %d, want exactly 1 (weekly idempotency)", got)
	}
	if bizRepo.updates != 1 {
		t.Errorf("last-sent stamp writes = %d, want 1", bizRepo.updates)
	}
}

// TestOwnerBrief_DueSelection asserts a business is dispatched only inside its
// weekday/hour window and only once its enabled+owner-channel gates pass.
func TestOwnerBrief_DueSelection(t *testing.T) {
	t.Run("outside window is skipped", func(t *testing.T) {
		offWindow := time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC) // Monday 15:00, not the 09:00 window
		router := &recordingRouter{reply: "x"}
		svc, _, nc, _, _ := newBriefFixture(t, offWindow, router, fakeBriefStats{stats: someStats()})
		if err := svc.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if nc.count() != 0 {
			t.Fatalf("dispatches = %d, want 0 outside the weekday/hour window", nc.count())
		}
	})

	t.Run("inside window sends", func(t *testing.T) {
		router := &recordingRouter{reply: "x"}
		svc, _, nc, _, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})
		if err := svc.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if nc.count() != 1 {
			t.Fatalf("dispatches = %d, want 1 inside the window", nc.count())
		}
	})
}

// TestOwnerBrief_OptOutSkip asserts ownerBrief.enabled=false skips the business.
func TestOwnerBrief_OptOutSkip(t *testing.T) {
	router := &recordingRouter{reply: "x"}
	svc, bizRepo, nc, _, bizID := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})
	bizRepo.businesses[bizID].Settings = map[string]interface{}{
		"ownerBrief": map[string]interface{}{"enabled": false},
	}

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if nc.count() != 0 {
		t.Fatalf("dispatches = %d, want 0 after opt-out", nc.count())
	}
}

// TestOwnerBrief_NoOwnerChannelSkip asserts a business whose telegram integration
// carries no private telegram_user_id is never dispatched (no send_notification
// attempt that would fail closed at the agent).
func TestOwnerBrief_NoOwnerChannelSkip(t *testing.T) {
	bizID := uuid.New()
	bizRepo := &fakeBriefBusinessRepo{
		businesses: map[uuid.UUID]*domain.Business{bizID: {ID: bizID, Name: "No Owner Id"}},
	}
	integRepo := &fakeBriefIntegRepo{integrations: []domain.Integration{
		telegramInteg(bizID, map[string]interface{}{}), // no telegram_user_id
	}}
	nc := &recordingRequester{}
	svc := NewOwnerBriefService(integRepo, bizRepo, fakeBriefStats{stats: someStats()}, &recordingRouter{reply: "x"}, "m", nc, &recordingTelemetry{})
	svc.now = mondayNineAM

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if nc.count() != 0 {
		t.Fatalf("dispatches = %d, want 0 with no private owner recipient", nc.count())
	}
}

// TestOwnerBrief_TenantScoping asserts the composed brief and the dispatched DM
// carry ONLY the acting business's own id and owner chat id — no cross-tenant
// bleed of BusinessID (metering) or chat_id (delivery).
func TestOwnerBrief_TenantScoping(t *testing.T) {
	bizA, bizB := uuid.New(), uuid.New()
	bizRepo := &fakeBriefBusinessRepo{businesses: map[uuid.UUID]*domain.Business{
		bizA: {ID: bizA, Name: "A"},
		bizB: {ID: bizB, Name: "B"},
	}}
	integRepo := &fakeBriefIntegRepo{integrations: []domain.Integration{
		telegramInteg(bizA, map[string]interface{}{"telegram_user_id": "111"}),
		telegramInteg(bizB, map[string]interface{}{"telegram_user_id": "222"}),
	}}
	router := &recordingRouter{reply: "x"}
	nc := &recordingRequester{}
	svc := NewOwnerBriefService(integRepo, bizRepo, fakeBriefStats{stats: someStats()}, router, "m", nc, &recordingTelemetry{})
	svc.now = mondayNineAM

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	router.mu.Lock()
	byBiz := map[uuid.UUID]string{}
	for _, req := range router.requests {
		byBiz[req.BusinessID] = req.Messages[len(req.Messages)-1].Content
	}
	router.mu.Unlock()
	if len(byBiz) != 2 || byBiz[bizA] == "" || byBiz[bizB] == "" {
		t.Fatalf("expected one metered compose per business, got %v", byBiz)
	}

	nc.mu.Lock()
	defer nc.mu.Unlock()
	chatByArgs := map[string]bool{}
	for _, m := range nc.sent {
		chatByArgs[m.args["chat_id"].(string)] = true
	}
	if !chatByArgs["111"] || !chatByArgs["222"] || len(chatByArgs) != 2 {
		t.Fatalf("expected exactly the two owner chat ids, got %v", chatByArgs)
	}
}

// TestOwnerBrief_TemplatedFallback asserts that when the metered LLM call errors
// (credit-denied via ErrRateLimitExceeded, or a generic provider outage) the
// brief degrades to the deterministic template, is STILL dispatched and stamped,
// and telemetry is STILL emitted with mode=template — never a silent no-send and
// never a forced/uncharged spend beyond the single metered attempt.
func TestOwnerBrief_TemplatedFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"credit denied", llm.ErrRateLimitExceeded},
		{"provider down", errors.New("upstream 503")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &recordingRouter{err: tc.err}
			svc, bizRepo, nc, tel, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})

			if err := svc.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			if nc.count() != 1 {
				t.Fatalf("dispatches = %d, want 1 (fallback must still send)", nc.count())
			}
			if bizRepo.updates != 1 {
				t.Errorf("last-sent stamp writes = %d, want 1 (fallback must still stamp)", bizRepo.updates)
			}
			nc.mu.Lock()
			body, _ := nc.sent[0].args["text"].(string)
			nc.mu.Unlock()
			if !strings.Contains(body, "Еженедельная сводка") {
				t.Errorf("fallback brief must be the deterministic template, got %q", body)
			}
			tel.mu.Lock()
			defer tel.mu.Unlock()
			if len(tel.events) != 1 || tel.events[0].Metadata["mode"] != "template" {
				t.Errorf("telemetry must record one mode=template event, got %+v", tel.events)
			}
		})
	}
}

// TestOwnerBrief_MeteringWiring asserts the composer sets ChatRequest.BusinessID
// (never uuid.Nil) so the WithBilling router meters the call — this is the entire
// "zero new billing code" guarantee.
func TestOwnerBrief_MeteringWiring(t *testing.T) {
	router := &recordingRouter{reply: "сводка"}
	svc, _, _, tel, bizID := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	router.mu.Lock()
	defer router.mu.Unlock()
	if len(router.requests) != 1 {
		t.Fatalf("router calls = %d, want 1", len(router.requests))
	}
	if router.requests[0].BusinessID != bizID {
		t.Errorf("ChatRequest.BusinessID = %v, want %v (metering fires only when set)", router.requests[0].BusinessID, bizID)
	}
	if router.requests[0].BusinessID == uuid.Nil {
		t.Error("ChatRequest.BusinessID must not be uuid.Nil — the Router skips billing on Nil")
	}
	tel.mu.Lock()
	defer tel.mu.Unlock()
	if len(tel.events) != 1 || tel.events[0].Metadata["mode"] != "llm" {
		t.Errorf("telemetry must record one mode=llm event, got %+v", tel.events)
	}
}

// TestOwnerBrief_PDnPromptSafety is the PDn guard: the composer prompt must carry
// ONLY aggregate numbers, never a raw author name, review text, or reply text.
func TestOwnerBrief_PDnPromptSafety(t *testing.T) {
	router := &recordingRouter{reply: "x"}
	svc, _, _, _, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	prompt := router.lastPromptText()
	for _, leaked := range []string{"author", "Иван", "review text", "reply_text", "AuthorName"} {
		if strings.Contains(prompt, leaked) {
			t.Errorf("composer prompt leaked raw review field %q: %s", leaked, prompt)
		}
	}
}

// TestOwnerBrief_FirstBriefOptOutLine asserts the FIRST brief (no prior send)
// carries the opt-out line, in the default (ru) locale.
func TestOwnerBrief_FirstBriefOptOutLine(t *testing.T) {
	router := &recordingRouter{reply: "Ваша сводка"}
	svc, _, nc, _, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	nc.mu.Lock()
	body, _ := nc.sent[0].args["text"].(string)
	nc.mu.Unlock()
	if !strings.Contains(body, "Отключить еженедельные сводки можно в настройках организации") {
		t.Errorf("first brief must carry the ru opt-out line, got %q", body)
	}
}

// TestOwnerBrief_DispatchTargetsSendNotification asserts the dispatch uses the
// send_notification tool on the telegram subject with the owner's private chat_id
// and a per-week approval id.
func TestOwnerBrief_DispatchTargetsSendNotification(t *testing.T) {
	router := &recordingRouter{reply: "x"}
	svc, _, nc, _, _ := newBriefFixture(t, mondayNineAM(), router, fakeBriefStats{stats: someStats()})

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if nc.sent[0].tool != tools.TelegramSendNotification {
		t.Errorf("tool = %q, want %q", nc.sent[0].tool, tools.TelegramSendNotification)
	}
	if nc.sent[0].subject != a2a.Subject(a2a.AgentTelegram) {
		t.Errorf("subject = %q, want %q", nc.sent[0].subject, a2a.Subject(a2a.AgentTelegram))
	}
	if nc.sent[0].args["chat_id"].(string) != "42" {
		t.Errorf("chat_id = %v, want the owner private id 42", nc.sent[0].args["chat_id"])
	}
}

// TestOwnerBrief_NilNATSNoOp asserts a Mongo-only deploy (nil NATS) never sends.
func TestOwnerBrief_NilNATSNoOp(t *testing.T) {
	bizID := uuid.New()
	bizRepo := &fakeBriefBusinessRepo{businesses: map[uuid.UUID]*domain.Business{bizID: {ID: bizID, Name: "X"}}}
	integRepo := &fakeBriefIntegRepo{integrations: []domain.Integration{telegramInteg(bizID, ownerMeta())}}
	svc := NewOwnerBriefService(integRepo, bizRepo, fakeBriefStats{stats: someStats()}, &recordingRouter{reply: "x"}, "m", nil, nil)
	svc.now = mondayNineAM
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce with nil NATS must be a no-op, got %v", err)
	}
	if bizRepo.updates != 0 {
		t.Errorf("nil-NATS pass must not stamp any business, updates = %d", bizRepo.updates)
	}
}

// --- accessor + compose unit tests ------------------------------------------

// TestComposeBrief_EnglishLocale asserts the composer honors a non-default
// locale and still appends the en opt-out line on the first brief.
func TestComposeBrief_EnglishLocale(t *testing.T) {
	router := &recordingRouter{reply: "Your weekly summary."}
	biz := &domain.Business{ID: uuid.New(), Name: "Corner Coffee"}
	text, usedLLM := composeBrief(context.Background(), router, "m", biz, someStats(), language.English, true)
	if !usedLLM {
		t.Fatal("expected LLM path")
	}
	if !strings.Contains(text, "turn off these weekly summaries") {
		t.Errorf("english first brief must carry the en opt-out line, got %q", text)
	}
}

// TestTemplatedBrief_DeterministicAndPDnSafe asserts the template renders honest
// numbers with no raw review fields, in both locales.
func TestTemplatedBrief_DeterministicAndPDnSafe(t *testing.T) {
	biz := &domain.Business{ID: uuid.New(), Name: "Corner Coffee"}
	ru := templatedBrief(biz, someStats(), language.Russian, false)
	en := templatedBrief(biz, someStats(), language.English, false)
	if !strings.Contains(ru, "Всего отзывов: 10") {
		t.Errorf("ru template missing total, got %q", ru)
	}
	if !strings.Contains(en, "Total reviews: 10") {
		t.Errorf("en template missing total, got %q", en)
	}
	for _, leaked := range []string{"author", "AuthorName", "reply_text"} {
		if strings.Contains(ru, leaked) || strings.Contains(en, leaked) {
			t.Errorf("template leaked raw field %q", leaked)
		}
	}
}

// TestIsoYearWeek_StableWithinWeek asserts the stamp is identical for two instants
// in the same ISO week and differs across weeks (idempotency key correctness).
func TestIsoYearWeek_StableWithinWeek(t *testing.T) {
	monday := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)
	sameWeek := time.Date(2026, 7, 8, 18, 0, 0, 0, time.UTC)
	nextWeek := time.Date(2026, 7, 13, 9, 0, 0, 0, time.UTC)
	if isoYearWeek(monday) != isoYearWeek(sameWeek) {
		t.Errorf("same-week stamps differ: %s vs %s", isoYearWeek(monday), isoYearWeek(sameWeek))
	}
	if isoYearWeek(monday) == isoYearWeek(nextWeek) {
		t.Errorf("next-week stamp must differ, both = %s", isoYearWeek(monday))
	}
}
