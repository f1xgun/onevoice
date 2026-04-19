package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// -- fakes -------------------------------------------------------------------

type fakeHITLPendingRepo struct {
	mu               sync.Mutex
	batches          map[string]*domain.PendingToolCallBatch
	resolvingWinners int32
	recordCalls      int32
}

func newFakeHITLPendingRepo() *fakeHITLPendingRepo {
	return &fakeHITLPendingRepo{batches: map[string]*domain.PendingToolCallBatch{}}
}

func (f *fakeHITLPendingRepo) InsertPreparing(_ context.Context, _ *domain.PendingToolCallBatch) error {
	return nil
}
func (f *fakeHITLPendingRepo) PromoteToPending(_ context.Context, _ string) error { return nil }
func (f *fakeHITLPendingRepo) GetByBatchID(_ context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.batches[batchID]
	if !ok {
		return nil, domain.ErrBatchNotFound
	}
	cpy := *b
	return &cpy, nil
}
func (f *fakeHITLPendingRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return nil, nil
}
func (f *fakeHITLPendingRepo) AtomicTransitionToResolving(_ context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.batches[batchID]
	if !ok {
		return nil, domain.ErrBatchNotFound
	}
	if b.Status != "pending" {
		return nil, domain.ErrBatchNotPending
	}
	b.Status = "resolving"
	atomic.AddInt32(&f.resolvingWinners, 1)
	return b, nil
}
func (f *fakeHITLPendingRepo) RecordDecisions(_ context.Context, batchID string, calls []domain.PendingCall) error {
	atomic.AddInt32(&f.recordCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.batches[batchID]; ok {
		cp := make([]domain.PendingCall, len(calls))
		copy(cp, calls)
		b.Calls = cp
	}
	return nil
}
func (f *fakeHITLPendingRepo) MarkDispatched(_ context.Context, _, _ string) error { return nil }
func (f *fakeHITLPendingRepo) MarkResolved(_ context.Context, _ string) error      { return nil }
func (f *fakeHITLPendingRepo) MarkExpired(_ context.Context, _ string) error       { return nil }
func (f *fakeHITLPendingRepo) ReconcileOrphanPreparing(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

type fakeBusinessRepoHITL struct {
	biz *domain.Business
}

func (f *fakeBusinessRepoHITL) Create(_ context.Context, _ *domain.Business) error { return nil }
func (f *fakeBusinessRepoHITL) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if f.biz == nil {
		return nil, domain.ErrBusinessNotFound
	}
	b := *f.biz
	return &b, nil
}
func (f *fakeBusinessRepoHITL) GetByUserID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	return nil, domain.ErrBusinessNotFound
}
func (f *fakeBusinessRepoHITL) Update(_ context.Context, _ *domain.Business) error { return nil }

type fakeProjectRepoHITL struct {
	proj *domain.Project
}

func (f *fakeProjectRepoHITL) Create(_ context.Context, _ *domain.Project) error { return nil }
func (f *fakeProjectRepoHITL) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
	if f.proj == nil {
		return nil, domain.ErrProjectNotFound
	}
	p := *f.proj
	return &p, nil
}
func (f *fakeProjectRepoHITL) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (f *fakeProjectRepoHITL) Update(_ context.Context, _ *domain.Project) error { return nil }
func (f *fakeProjectRepoHITL) Delete(_ context.Context, _ uuid.UUID) error       { return nil }
func (f *fakeProjectRepoHITL) CountConversationsByID(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeProjectRepoHITL) HardDeleteCascade(_ context.Context, _ uuid.UUID) (int, int, error) {
	return 0, 0, nil
}

// hitlBusinessService stubs BusinessService for the handler-level tests.
type hitlBusinessService struct {
	biz *domain.Business
}

func (s *hitlBusinessService) Create(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *hitlBusinessService) GetByUserID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if s.biz == nil {
		return nil, domain.ErrBusinessNotFound
	}
	return s.biz, nil
}
func (s *hitlBusinessService) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if s.biz == nil {
		return nil, domain.ErrBusinessNotFound
	}
	return s.biz, nil
}
func (s *hitlBusinessService) Update(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}

type hitlConvRepo struct {
	convs map[string]*domain.Conversation
}

func (c *hitlConvRepo) Create(_ context.Context, _ *domain.Conversation) error { return nil }
func (c *hitlConvRepo) GetByID(_ context.Context, id string) (*domain.Conversation, error) {
	if c.convs == nil {
		return nil, domain.ErrConversationNotFound
	}
	conv, ok := c.convs[id]
	if !ok {
		return nil, domain.ErrConversationNotFound
	}
	return conv, nil
}
func (c *hitlConvRepo) ListByUserID(_ context.Context, _ string, _, _ int) ([]domain.Conversation, error) {
	return nil, nil
}
func (c *hitlConvRepo) Update(_ context.Context, _ *domain.Conversation) error { return nil }
func (c *hitlConvRepo) Delete(_ context.Context, _ string) error               { return nil }
func (c *hitlConvRepo) UpdateProjectAssignment(_ context.Context, _ string, _ *string) error {
	return nil
}

// -- helpers -----------------------------------------------------------------

func seededToolsCache() *service.ToolsRegistryCache {
	cache := service.NewToolsRegistryCache("", nil, time.Minute)
	cache.Seed([]service.ToolsRegistryEntry{
		{
			Name:           "telegram__send_channel_post",
			Platform:       "telegram",
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text", "parse_mode"},
			Description:    "telegram post",
		},
		{
			Name:           "vk__publish_post",
			Platform:       "vk",
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
			Description:    "vk post",
		},
	})
	return cache
}

func buildHITLHandler(t *testing.T, pr *fakeHITLPendingRepo, biz *domain.Business, proj *domain.Project, orchURL string) *handler.HITLHandler {
	t.Helper()
	svc := service.NewHITLService(
		pr,
		&fakeBusinessRepoHITL{biz: biz},
		&fakeProjectRepoHITL{proj: proj},
		seededToolsCache(),
		orchURL,
		http.DefaultClient,
	)
	h, err := handler.NewHITLHandler(svc, &hitlBusinessService{biz: biz}, &hitlConvRepo{})
	if err != nil {
		t.Fatalf("NewHITLHandler: %v", err)
	}
	return h
}

func seedHandlerBatch(pr *fakeHITLPendingRepo, batchID, convID, bizID string, calls []domain.PendingCall) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.batches[batchID] = &domain.PendingToolCallBatch{
		ID:             batchID,
		ConversationID: convID,
		BusinessID:     bizID,
		Status:         "pending",
		Calls:          calls,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
}

// hitlRouteRequest wires chi route params and the auth context then invokes
// ResolvePendingToolCalls. Returns the recorder for assertions.
func hitlRouteRequest(t *testing.T, h *handler.HITLHandler, userID uuid.UUID, convID, batchID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/conversations/%s/pending-tool-calls/%s/resolve", convID, batchID),
		bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	r := chi.NewRouter()
	r.Post("/api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve", h.ResolvePendingToolCalls)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// -- resolve tests -----------------------------------------------------------

func TestResolve_Happy_Returns200WithDecisions(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
		{CallID: "tc_b", ToolName: "vk__publish_post", Arguments: map[string]interface{}{"text": "yo"}},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "approve"},
			{"id": "tc_b", "action": "approve"},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["batch_id"] != "b1" {
		t.Errorf("batch_id = %v", resp["batch_id"])
	}
	decisions, _ := resp["decisions"].([]interface{})
	if len(decisions) != 2 {
		t.Errorf("decisions len = %d, want 2", len(decisions))
	}
}

func TestResolve_Missing_Returns404(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{"decisions": []interface{}{}})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "ghost", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestResolve_CrossTenant_Returns403(t *testing.T) {
	attacker := &domain.Business{ID: uuid.New()}
	ownerID := uuid.New().String()
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", ownerID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	h := buildHITLHandler(t, pr, attacker, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{{"id": "tc_a", "action": "approve"}},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestResolve_PartialDecisions_Returns400WithMissing(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
		{CallID: "tc_b", ToolName: "vk__publish_post"},
		{CallID: "tc_c", ToolName: "telegram__send_channel_post"},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "approve"},
			{"id": "tc_b", "action": "approve"},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	missing, _ := resp["missing"].([]interface{})
	if len(missing) != 1 || missing[0] != "tc_c" {
		t.Errorf("missing = %v, want [tc_c]", missing)
	}
}

// TestResolve_EditInvalidField_Returns400WithEditable — anti-footgun #6
// MANDATORY test. Asserts exact body shape {error, editable}.
func TestResolve_EditInvalidField_Returns400WithEditable(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "edit", "edited_args": map[string]interface{}{"channel_id": "-100"}},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if _, ok := resp["error"].(string); !ok {
		t.Errorf("missing error key: %v", resp)
	}
	editable, ok := resp["editable"].([]interface{})
	if !ok {
		t.Fatalf("editable key missing or not array: %v", resp)
	}
	if len(editable) != 2 {
		t.Fatalf("editable len = %d, want 2: %v", len(editable), editable)
	}
	if editable[0].(string) != "text" || editable[1].(string) != "parse_mode" {
		t.Errorf("editable = %v, want [text parse_mode]", editable)
	}
}

func TestResolve_EditCaseMismatch_Returns400(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "edit", "edited_args": map[string]interface{}{"Text": "x"}},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestResolve_EditNestedObject_Returns400(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "edit", "edited_args": map[string]interface{}{"text": map[string]interface{}{"nested": 1}}},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestResolve_RejectReasonTooLong_Returns400(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "reject", "reject_reason": strings.Repeat("x", 501)},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestResolve_ConcurrentResolve_ExactlyOneWins_OtherGets409 — MANDATORY
// anti-footgun #5 test. sync.WaitGroup + two concurrent requests; asserts
// exactly one 200 and one 409 with the correct body shape.
func TestResolve_ConcurrentResolve_ExactlyOneWins_OtherGets409(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{{"id": "tc_a", "action": "approve"}},
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	codes := make(chan int, 2)
	bodies := make(chan string, 2)

	userID := uuid.New()
	worker := func() {
		defer wg.Done()
		<-start
		rec := hitlRouteRequest(t, h, userID, "c1", "b1", bodyBytes)
		codes <- rec.Code
		bodies <- rec.Body.String()
	}
	go worker()
	go worker()
	close(start)
	wg.Wait()
	close(codes)
	close(bodies)

	var wins, conflicts int
	var conflictBody string
	for code := range codes {
		switch code {
		case http.StatusOK:
			wins++
		case http.StatusConflict:
			conflicts++
		}
	}
	for body := range bodies {
		if strings.Contains(body, "batch resolving") {
			conflictBody = body
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent resolve: wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
	if !strings.Contains(conflictBody, `"retry_after_ms":500`) {
		t.Errorf("409 body missing retry_after_ms: %s", conflictBody)
	}
	if !strings.Contains(conflictBody, "concurrent resolve in progress") {
		t.Errorf("409 body missing reason: %s", conflictBody)
	}
}

// TestResolve_TOCTOU_PolicyFlipsToForbidden_RewritesToReject — HITL-06 invariant.
func TestResolve_TOCTOU_PolicyFlipsToForbidden_RewritesToReject(t *testing.T) {
	bizUUID := uuid.New()
	biz := &domain.Business{
		ID: bizUUID,
		Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				"telegram__send_channel_post": "forbidden",
			},
		},
	}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", bizUUID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{{"id": "tc_a", "action": "approve"}},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	decisions := resp["decisions"].([]interface{})
	first := decisions[0].(map[string]interface{})
	if first["action"] != "reject" {
		t.Errorf("action = %v, want reject", first["action"])
	}
	if first["reason"] != "policy_revoked" {
		t.Errorf("reason = %v, want policy_revoked", first["reason"])
	}
	// Persisted batch must reflect the rewrite.
	b, _ := pr.GetByBatchID(context.Background(), "b1")
	if b.Calls[0].Verdict != "reject" {
		t.Errorf("persisted Verdict = %q, want reject", b.Calls[0].Verdict)
	}
	if b.Calls[0].RejectReason != "policy_revoked" {
		t.Errorf("persisted RejectReason = %q", b.Calls[0].RejectReason)
	}
}

// TestResolve_ClientTamperedToolName_IgnoredAndPinned — HITL-07 pinning.
func TestResolve_ClientTamperedToolName_IgnoredAndPinned(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	h := buildHITLHandler(t, pr, biz, nil, "")

	body, _ := json.Marshal(map[string]interface{}{
		"decisions": []map[string]interface{}{
			{"id": "tc_a", "action": "edit", "edited_args": map[string]interface{}{
				"tool_name": "telegram__send_channel_photo",
			}},
		},
	})
	rec := hitlRouteRequest(t, h, uuid.New(), "c1", "b1", body)
	// Expect 400 because "tool_name" is not in EditableFields.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	b, _ := pr.GetByBatchID(context.Background(), "b1")
	if b.Calls[0].ToolName != "telegram__send_channel_post" {
		t.Fatalf("tool_name mutated: got %q", b.Calls[0].ToolName)
	}
}

// -- resume tests ------------------------------------------------------------

func TestResume_BatchResolving_Returns409(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	// Move batch to status=resolving.
	pr.mu.Lock()
	pr.batches["b1"].Status = "resolving"
	pr.mu.Unlock()
	h := buildHITLHandler(t, pr, biz, nil, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/c1/resume?batch_id=b1", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	req = req.WithContext(ctx)
	r := chi.NewRouter()
	r.Post("/api/v1/chat/{id}/resume", h.Resume)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "batch resolving") {
		t.Errorf("body missing reason: %s", rec.Body.String())
	}
}

func TestResume_Expired_Returns410(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	pr.mu.Lock()
	pr.batches["b1"].Status = "expired"
	pr.mu.Unlock()
	h := buildHITLHandler(t, pr, biz, nil, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/c1/resume?batch_id=b1", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	req = req.WithContext(ctx)
	r := chi.NewRouter()
	r.Post("/api/v1/chat/{id}/resume", h.Resume)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
}

func TestResume_Happy_OpensSSEStream_ForwardingOrchestratorEvents(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()
	seedHandlerBatch(pr, "b1", "c1", biz.ID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})

	// Mock orchestrator resume endpoint.
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/chat/c1/resume") {
			t.Errorf("orchestrator path = %q", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "business_approvals") {
			t.Errorf("body missing business_approvals: %s", string(raw))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, `data: {"type":"tool_result","tool_call_id":"tc_a","result":{"ok":true}}`+"\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, `data: {"type":"done"}`+"\n\n")
		flusher.Flush()
	}))
	defer orch.Close()

	h := buildHITLHandler(t, pr, biz, nil, orch.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/c1/resume?batch_id=b1", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	req = req.WithContext(ctx)
	r := chi.NewRouter()
	r.Post("/api/v1/chat/{id}/resume", h.Resume)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"tool_result"`) {
		t.Errorf("missing tool_result event: %s", body)
	}
	if !strings.Contains(body, `"type":"done"`) {
		t.Errorf("missing done event: %s", body)
	}
}

// -- GET /tools tests --------------------------------------------------------

func TestGetTools_ReturnsRegistryProjection(t *testing.T) {
	biz := &domain.Business{ID: uuid.New()}
	pr := newFakeHITLPendingRepo()

	// Mock orchestrator /internal/tools — proves the cache actually fetches.
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/internal/tools") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"telegram__send_channel_post","platform":"telegram","floor":"manual","editableFields":["text"],"description":"t"},
			{"name":"vk__publish_post","platform":"vk","floor":"manual","editableFields":["text"],"description":"v"}
		]`))
	}))
	defer orch.Close()

	cache := service.NewToolsRegistryCache(orch.URL, orch.Client(), 1*time.Second)
	svc := service.NewHITLService(
		pr,
		&fakeBusinessRepoHITL{biz: biz},
		&fakeProjectRepoHITL{},
		cache,
		orch.URL,
		orch.Client(),
	)
	h, err := handler.NewHITLHandler(svc, &hitlBusinessService{biz: biz}, &hitlConvRepo{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, uuid.New())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.GetTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var entries []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2: %v", len(entries), entries)
	}
	for i, e := range entries {
		for _, k := range []string{"name", "platform", "floor", "editableFields", "description"} {
			if _, ok := e[k]; !ok {
				t.Fatalf("entry[%d] missing %q: %v", i, k, e)
			}
		}
	}
}
