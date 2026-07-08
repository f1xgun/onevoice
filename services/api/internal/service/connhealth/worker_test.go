package connhealth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// fakeNATS records dispatched tool requests and returns a canned success reply.
type fakeNATS struct {
	mu         sync.Mutex
	dispatches []a2a.ToolRequest
}

func (f *fakeNATS) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var req a2a.ToolRequest
	_ = json.Unmarshal(msg.Data, &req)
	f.dispatches = append(f.dispatches, req)
	resp, _ := json.Marshal(a2a.ToolResponse{Success: true})
	return &natslib.Msg{Data: resp}, nil
}

func (f *fakeNATS) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dispatches)
}

// yandexReplyStore is a fakeStore whose ListAllActiveByPlatforms serves both the
// Yandex integration under test and an owner-bearing Telegram row.
type yandexReplyStore struct {
	rows    []domain.Integration
	updated map[uuid.UUID]map[string]interface{}
	mu      sync.Mutex
}

func (s *yandexReplyStore) ListAllActiveByPlatforms(_ context.Context, _ []string) ([]domain.Integration, error) {
	return s.rows, nil
}

// SetMetadataKeys mirrors the repository's targeted jsonb_set: it merges the
// given top-level keys into the row's CURRENT metadata, replacing each supplied
// key wholesale and preserving every sibling (e.g. telegram_user_id).
func (s *yandexReplyStore) SetMetadataKeys(_ context.Context, id uuid.UUID, keys map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updated == nil {
		s.updated = map[uuid.UUID]map[string]interface{}{}
	}
	merged := map[string]interface{}{}
	for i := range s.rows {
		if s.rows[i].ID == id {
			for k, v := range s.rows[i].Metadata {
				merged[k] = v
			}
		}
	}
	for k, v := range keys {
		merged[k] = v
	}
	s.updated[id] = merged
	s.rows = applyMetadata(s.rows, id, merged)
	return nil
}
func (s *yandexReplyStore) get(id uuid.UUID) map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updated[id]
}

func applyMetadata(rows []domain.Integration, id uuid.UUID, m map[string]interface{}) []domain.Integration {
	for i := range rows {
		if rows[i].ID == id {
			rows[i].Metadata = m
		}
	}
	return rows
}

// checkerStore lets the embedded Checker read the same rows the worker sees.
type checkerStore struct{ parent *yandexReplyStore }

func (c *checkerStore) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}
func (c *checkerStore) SetMetadataKeys(ctx context.Context, id uuid.UUID, keys map[string]interface{}) error {
	return c.parent.SetMetadataKeys(ctx, id, keys)
}

func newYandexWorker(t *testing.T, dispatch Dispatcher, nc natsRequester, rows []domain.Integration) (*Worker, *yandexReplyStore) {
	t.Helper()
	store := &yandexReplyStore{rows: rows}
	checker := NewChecker(nil, dispatch, &checkerStore{parent: store}, nil)
	w := NewWorker(store, checker, nc)
	return w, store
}

func yandexRow(bizID, id uuid.UUID, meta map[string]interface{}) domain.Integration {
	return domain.Integration{ID: id, BusinessID: bizID, Platform: a2a.AgentYandexBusiness, ExternalID: "org-1", Metadata: meta}
}

func telegramOwnerRow(bizID uuid.UUID, chatID string) domain.Integration {
	return domain.Integration{
		ID: uuid.New(), BusinessID: bizID, Platform: a2a.AgentTelegram, ExternalID: "-100",
		Metadata: map[string]interface{}{"telegram_user_id": chatID},
	}
}

func TestWorker_TransitionToBroken_NudgesOnce(t *testing.T) {
	bizID, yID := uuid.New(), uuid.New()
	priorActive := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	rows := []domain.Integration{
		yandexRow(bizID, yID, priorActive),
		telegramOwnerRow(bizID, "555"),
	}
	nc := &fakeNATS{}
	dispatch := &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))}
	w, store := newYandexWorker(t, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce pass 1: %v", err)
	}
	if nc.count() != 1 {
		t.Fatalf("expected exactly one nudge on transition to broken, got %d", nc.count())
	}
	sent := nc.dispatches[0]
	if sent.Tool != tools.TelegramSendNotification {
		t.Fatalf("expected send_notification, got %q", sent.Tool)
	}
	if sent.Args["chat_id"] != "555" {
		t.Fatalf("expected owner chat 555, got %v", sent.Args["chat_id"])
	}
	if ReadNudgedAt(store.get(yID)).IsZero() {
		t.Fatalf("expected nudged_at stamped after successful nudge")
	}

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce pass 2: %v", err)
	}
	if nc.count() != 1 {
		t.Fatalf("expected NO re-nudge while still broken, got %d total", nc.count())
	}
}

func TestWorker_TransitionToBroken_PersistsBrokenAfterNudgeStamp(t *testing.T) {
	bizID, yID := uuid.New(), uuid.New()
	priorActive := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	rows := []domain.Integration{
		yandexRow(bizID, yID, priorActive),
		telegramOwnerRow(bizID, "555"),
	}
	nc := &fakeNATS{}
	dispatch := &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))}
	w, store := newYandexWorker(t, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if nc.count() != 1 {
		t.Fatalf("expected one nudge on transition to broken, got %d", nc.count())
	}
	persisted := ReadFromMetadata(store.get(yID))
	if persisted.Status != StatusBroken {
		t.Fatalf("the nudge stamp must layer on the fresh broken verdict, got persisted status %q (dashboard would show healthy while the owner got a reconnect DM)", persisted.Status)
	}
	if ReadNudgedAt(store.get(yID)).IsZero() {
		t.Fatalf("expected nudged_at stamped alongside the broken verdict")
	}
}

func TestWorker_Recovery_PersistsActiveAfterNudgeClear(t *testing.T) {
	bizID, yID := uuid.New(), uuid.New()
	brokenNudged := MergeNudgedAt(
		MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: time.Now().Add(-time.Hour)}),
		time.Now().Add(-2*time.Hour),
	)
	rows := []domain.Integration{
		yandexRow(bizID, yID, brokenNudged),
		telegramOwnerRow(bizID, "555"),
	}
	nc := &fakeNATS{}
	dispatch := &fakeDispatcher{resp: &a2a.ToolResponse{Success: true}}
	w, store := newYandexWorker(t, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	persisted := ReadFromMetadata(store.get(yID))
	if persisted.Status != StatusActive {
		t.Fatalf("clearing nudged_at must layer on the fresh active verdict, got persisted status %q (a recovered channel stuck broken forever)", persisted.Status)
	}
	if !ReadNudgedAt(store.get(yID)).IsZero() {
		t.Fatalf("expected nudged_at cleared on recovery")
	}
}

func TestWorker_RecoveryClearsNudgedAt(t *testing.T) {
	bizID, yID := uuid.New(), uuid.New()
	brokenNudged := MergeNudgedAt(
		MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: time.Now().Add(-time.Hour)}),
		time.Now().Add(-2*time.Hour),
	)
	rows := []domain.Integration{
		yandexRow(bizID, yID, brokenNudged),
		telegramOwnerRow(bizID, "555"),
	}
	nc := &fakeNATS{}
	dispatch := &fakeDispatcher{resp: &a2a.ToolResponse{Success: true}}
	w, store := newYandexWorker(t, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !ReadNudgedAt(store.get(yID)).IsZero() {
		t.Fatalf("expected nudged_at cleared on recovery to active")
	}
	if nc.count() != 0 {
		t.Fatalf("expected no nudge on recovery, got %d", nc.count())
	}
}

func TestWorker_NoBoundOwnerChat_RecordsHealthNoDispatch(t *testing.T) {
	bizID, yID := uuid.New(), uuid.New()
	priorActive := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	rows := []domain.Integration{yandexRow(bizID, yID, priorActive)}
	nc := &fakeNATS{}
	dispatch := &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("session expired"))}
	w, store := newYandexWorker(t, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if nc.count() != 0 {
		t.Fatalf("expected no dispatch without a bound owner chat, got %d", nc.count())
	}
	if ReadFromMetadata(store.get(yID)).Status != StatusBroken {
		t.Fatalf("expected health recorded broken even without owner chat")
	}
}

func TestWorker_YandexSessionExpired_MapsBroken_TimeoutUnknown(t *testing.T) {
	bizID, yID := uuid.New(), uuid.New()
	rows := []domain.Integration{yandexRow(bizID, yID, nil)}

	brokenW, brokenStore := newYandexWorker(t, &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))}, &fakeNATS{}, rows)
	if err := brokenW.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce broken: %v", err)
	}
	if ReadFromMetadata(brokenStore.get(yID)).Status != StatusBroken {
		t.Fatalf("expected broken on coded token-invalid")
	}

	rows2 := []domain.Integration{yandexRow(bizID, yID, nil)}
	unknownW, unknownStore := newYandexWorker(t, &fakeDispatcher{err: errors.New("nats request: context deadline exceeded")}, &fakeNATS{}, rows2)
	if err := unknownW.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce timeout: %v", err)
	}
	if ReadFromMetadata(unknownStore.get(yID)).Status != StatusUnknown {
		t.Fatalf("expected fail-soft unknown on NATS timeout")
	}
}

func TestWorker_NilNC_NoOp(t *testing.T) {
	w, _ := newYandexWorker(t, &fakeDispatcher{resp: &a2a.ToolResponse{Success: true}}, nil, nil)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("expected nil (no-op) in Mongo-only mode, got %v", err)
	}
}

// newMultiWorker builds a worker whose checker probes Telegram/VK via probe and
// Yandex via dispatch, all reading the same rows the worker enumerates.
func newMultiWorker(t *testing.T, probe PlatformProbe, dispatch Dispatcher, nc natsRequester, rows []domain.Integration) (*Worker, *yandexReplyStore) {
	t.Helper()
	store := &yandexReplyStore{rows: rows}
	checker := NewChecker(probe, dispatch, &checkerStore{parent: store}, nil)
	w := NewWorker(store, checker, nc)
	return w, store
}

func telegramChannelRow(bizID, id uuid.UUID, meta map[string]interface{}) domain.Integration {
	return domain.Integration{ID: id, BusinessID: bizID, Platform: a2a.AgentTelegram, ExternalID: "-100777", Metadata: meta}
}

func vkRow(bizID, id uuid.UUID, meta map[string]interface{}) domain.Integration {
	return domain.Integration{ID: id, BusinessID: bizID, Platform: a2a.AgentVK, ExternalID: "236912172", Metadata: meta}
}

// TestWorker_TelegramBreak_PersistsHealth_NoNudge: a Telegram channel that
// probes broken has its verdict persisted (so the dashboard badge is live) but
// is NOT DM-nudged — even though the same row carries an owner chat, proving the
// nudge-eligibility gate (not a missing chat) suppresses the DM.
func TestWorker_TelegramBreak_PersistsHealth_NoNudge(t *testing.T) {
	bizID, tgID := uuid.New(), uuid.New()
	meta := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	meta["telegram_user_id"] = "555"
	rows := []domain.Integration{telegramChannelRow(bizID, tgID, meta)}
	nc := &fakeNATS{}
	probe := &fakeProbe{tg: Result{Status: StatusBroken, ReasonCode: ReasonTelegramNotAdmin, CheckedAt: time.Now()}}
	w, store := newMultiWorker(t, probe, &fakeDispatcher{}, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := ReadFromMetadata(store.get(tgID)).Status; got != StatusBroken {
		t.Fatalf("expected the Telegram break persisted for a live badge, got %q", got)
	}
	if nc.count() != 0 {
		t.Fatalf("Telegram is not nudge-eligible: expected 0 owner DMs even with a bound chat, got %d", nc.count())
	}
}

// TestWorker_VKBreak_PersistsHealth_NoNudge: a VK community that probes broken
// records health but is not DM-nudged, with an owner chat available.
func TestWorker_VKBreak_PersistsHealth_NoNudge(t *testing.T) {
	bizID, vkID := uuid.New(), uuid.New()
	priorActive := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	rows := []domain.Integration{
		vkRow(bizID, vkID, priorActive),
		telegramOwnerRow(bizID, "555"),
	}
	nc := &fakeNATS{}
	probe := &fakeProbe{
		vk: Result{Status: StatusBroken, ReasonCode: ReasonVKTokenInvalid, CheckedAt: time.Now()},
		tg: Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()},
	}
	w, store := newMultiWorker(t, probe, &fakeDispatcher{}, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := ReadFromMetadata(store.get(vkID)).Status; got != StatusBroken {
		t.Fatalf("expected the VK break persisted for a live badge, got %q", got)
	}
	if nc.count() != 0 {
		t.Fatalf("VK is not nudge-eligible: expected 0 owner DMs, got %d", nc.count())
	}
}

// TestWorker_MultiPlatform_ProbesAll_NudgesYandexOnly: one pass probes and
// persists Telegram, VK, and Yandex health, and DMs the owner exactly once —
// for the Yandex break only.
func TestWorker_MultiPlatform_ProbesAll_NudgesYandexOnly(t *testing.T) {
	bizID := uuid.New()
	yID, tgID, vkID := uuid.New(), uuid.New(), uuid.New()
	prior := func() map[string]interface{} {
		return MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	}
	tgMeta := prior()
	tgMeta["telegram_user_id"] = "555"
	rows := []domain.Integration{
		yandexRow(bizID, yID, prior()),
		telegramChannelRow(bizID, tgID, tgMeta),
		vkRow(bizID, vkID, prior()),
	}
	nc := &fakeNATS{}
	probe := &fakeProbe{
		tg: Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()},
		vk: Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()},
	}
	dispatch := &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))}
	w, store := newMultiWorker(t, probe, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := ReadFromMetadata(store.get(yID)).Status; got != StatusBroken {
		t.Fatalf("expected Yandex probed broken, got %q", got)
	}
	if got := ReadFromMetadata(store.get(tgID)).Status; got != StatusActive {
		t.Fatalf("expected Telegram probed+persisted active, got %q", got)
	}
	if got := ReadFromMetadata(store.get(vkID)).Status; got != StatusActive {
		t.Fatalf("expected VK probed+persisted active, got %q", got)
	}
	if nc.count() != 1 {
		t.Fatalf("expected exactly one nudge (Yandex only), got %d", nc.count())
	}
	if nc.dispatches[0].Args["chat_id"] != "555" {
		t.Fatalf("expected the Yandex nudge to the bound owner chat 555, got %v", nc.dispatches[0].Args["chat_id"])
	}
}

// TestWorker_TelegramFlaky_KeepsPriorActive_NoNudge proves fail-soft holds for
// the newly-enabled Telegram path when driven THROUGH RunOnce: an inconclusive
// (Unknown) probe over a prior-active channel keeps it active and never nudges.
func TestWorker_TelegramFlaky_KeepsPriorActive_NoNudge(t *testing.T) {
	bizID, tgID := uuid.New(), uuid.New()
	meta := MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	meta["telegram_user_id"] = "555"
	rows := []domain.Integration{telegramChannelRow(bizID, tgID, meta)}
	nc := &fakeNATS{}
	probe := &fakeProbe{tg: Result{Status: StatusUnknown, ReasonCode: ReasonInconclusive, CheckedAt: time.Now()}}
	w, store := newMultiWorker(t, probe, &fakeDispatcher{}, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := ReadFromMetadata(store.get(tgID)).Status; got != StatusActive {
		t.Fatalf("fail-soft: an Unknown Telegram probe must keep the prior active verdict, got %q", got)
	}
	if nc.count() != 0 {
		t.Fatalf("expected no nudge on an inconclusive probe, got %d", nc.count())
	}
}

// TestWorker_HealthWrite_PreservesSiblingKeys is the end-to-end clobber guard:
// persisting a Telegram health verdict must leave connect-critical sibling keys
// (telegram_user_id, channel_title) on the same row intact — a regression would
// silently un-bind the owner and disable Yandex reconnect DMs.
func TestWorker_HealthWrite_PreservesSiblingKeys(t *testing.T) {
	bizID, tgID := uuid.New(), uuid.New()
	meta := map[string]interface{}{"telegram_user_id": "555", "channel_title": "Кафе"}
	rows := []domain.Integration{telegramChannelRow(bizID, tgID, meta)}
	nc := &fakeNATS{}
	probe := &fakeProbe{tg: Result{Status: StatusBroken, ReasonCode: ReasonTelegramNotAdmin, CheckedAt: time.Now()}}
	w, store := newMultiWorker(t, probe, &fakeDispatcher{}, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.get(tgID)
	if got["telegram_user_id"] != "555" {
		t.Fatalf("health write clobbered telegram_user_id (owner unbound), got %v", got["telegram_user_id"])
	}
	if got["channel_title"] != "Кафе" {
		t.Fatalf("health write clobbered channel_title, got %v", got["channel_title"])
	}
	if ReadFromMetadata(got).Status != StatusBroken {
		t.Fatalf("expected the health verdict persisted alongside the preserved siblings")
	}
}

// TestWorker_TelegramRecovery_DoesNotClearYandexNudge: a Telegram row recovering
// to active in the same pass must not disturb a Yandex row's owner-nudge stamp
// (the nudge gate returns before the clear branch for ineligible platforms).
func TestWorker_TelegramRecovery_DoesNotClearYandexNudge(t *testing.T) {
	bizID, yID, tgID := uuid.New(), uuid.New(), uuid.New()
	yandexBrokenNudged := MergeNudgedAt(
		MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonYandexSessionExpiry, CheckedAt: time.Now().Add(-time.Hour)}),
		time.Now().Add(-2*time.Hour),
	)
	tgPrevBroken := MergeIntoMetadata(nil, Result{Status: StatusBroken, ReasonCode: ReasonTelegramNotAdmin, CheckedAt: time.Now().Add(-time.Hour)})
	tgPrevBroken["telegram_user_id"] = "555"
	rows := []domain.Integration{
		yandexRow(bizID, yID, yandexBrokenNudged),
		telegramChannelRow(bizID, tgID, tgPrevBroken),
	}
	nc := &fakeNATS{}
	// Yandex still broken (no re-nudge, no clear); Telegram probes back to active.
	dispatch := &fakeDispatcher{err: a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))}
	probe := &fakeProbe{tg: Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()}}
	w, store := newMultiWorker(t, probe, dispatch, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if ReadNudgedAt(store.get(yID)).IsZero() {
		t.Fatalf("a Telegram recovery must NOT clear the Yandex owner-nudge stamp")
	}
	if got := ReadFromMetadata(store.get(tgID)).Status; got != StatusActive {
		t.Fatalf("expected the Telegram recovery persisted active, got %q", got)
	}
	if nc.count() != 0 {
		t.Fatalf("expected no nudge this pass (Yandex already broken+nudged), got %d", nc.count())
	}
}

// perBizDispatcher fails the Yandex canary only for one business, so multi-tenant
// nudge isolation can be exercised.
type perBizDispatcher struct{ brokenBiz string }

func (d *perBizDispatcher) RequestTool(_ context.Context, _ string, req a2a.ToolRequest, _ time.Duration) (*a2a.ToolResponse, error) {
	if req.BusinessID == d.brokenBiz {
		return nil, a2a.NewCodedError(codeIntegrationTokenInvalid, errors.New("passport.yandex"))
	}
	return &a2a.ToolResponse{Success: true}, nil
}

// TestWorker_TwoBusinesses_NudgeIsolation: with two businesses each holding a
// Yandex row and a bound owner, only the business whose Yandex session broke is
// DMed, and only on its own owner chat — no cross-tenant leakage.
func TestWorker_TwoBusinesses_NudgeIsolation(t *testing.T) {
	bizA, bizB := uuid.New(), uuid.New()
	yA, yB := uuid.New(), uuid.New()
	prior := func() map[string]interface{} {
		return MergeIntoMetadata(nil, Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now().Add(-time.Hour)})
	}
	rows := []domain.Integration{
		yandexRow(bizA, yA, prior()),
		telegramOwnerRow(bizA, "111"),
		yandexRow(bizB, yB, prior()),
		telegramOwnerRow(bizB, "222"),
	}
	nc := &fakeNATS{}
	probe := &fakeProbe{tg: Result{Status: StatusActive, ReasonCode: ReasonOK, CheckedAt: time.Now()}}
	w, _ := newMultiWorker(t, probe, &perBizDispatcher{brokenBiz: bizA.String()}, nc, rows)

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if nc.count() != 1 {
		t.Fatalf("expected exactly one nudge (only business A's Yandex broke), got %d", nc.count())
	}
	if nc.dispatches[0].Args["chat_id"] != "111" {
		t.Fatalf("cross-tenant leak: nudge must go to business A's owner 111, got %v", nc.dispatches[0].Args["chat_id"])
	}
}
