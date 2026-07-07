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
func (s *yandexReplyStore) UpdateMetadata(_ context.Context, id uuid.UUID, m map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updated == nil {
		s.updated = map[uuid.UUID]map[string]interface{}{}
	}
	s.updated[id] = m
	s.rows = applyMetadata(s.rows, id, m)
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
func (c *checkerStore) UpdateMetadata(ctx context.Context, id uuid.UUID, m map[string]interface{}) error {
	return c.parent.UpdateMetadata(ctx, id, m)
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
