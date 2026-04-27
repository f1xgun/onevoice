package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// titlerRoute wires the chi URL param `id` and the user ID context the way
// the production router does, then invokes RegenerateTitle.
func titlerRoute(t *testing.T, h *TitlerHandler, userID uuid.UUID, conversationID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/conversations/"+conversationID+"/regenerate-title", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", conversationID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.RegenerateTitle(rec, req)
	return rec
}

// titlerConvRepo is a focused fake that records UpdateTitleIfPending /
// TransitionToAutoPending calls. Other methods return safe defaults so the
// handler under test exercises only the regenerate path.
type titlerConvRepo struct {
	mu sync.Mutex
	// configured behavior:
	getByIDReturn  *domain.Conversation
	getByIDErr     error
	transitionErr  error
	updateTitleErr error

	// recorded behavior:
	transitionCalls  int
	updateTitleCalls []struct{ ID, Title string }
}

func (r *titlerConvRepo) Create(_ context.Context, _ *domain.Conversation) error { return nil }
func (r *titlerConvRepo) GetByID(_ context.Context, _ string) (*domain.Conversation, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	if r.getByIDReturn == nil {
		return nil, domain.ErrConversationNotFound
	}
	cp := *r.getByIDReturn
	return &cp, nil
}
func (r *titlerConvRepo) ListByUserID(_ context.Context, _ string, _, _ int) ([]domain.Conversation, error) {
	return nil, nil
}
func (r *titlerConvRepo) Update(_ context.Context, _ *domain.Conversation) error { return nil }
func (r *titlerConvRepo) Delete(_ context.Context, _ string) error               { return nil }
func (r *titlerConvRepo) UpdateProjectAssignment(_ context.Context, _ string, _ *string) error {
	return nil
}
func (r *titlerConvRepo) UpdateTitleIfPending(_ context.Context, id, title string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateTitleCalls = append(r.updateTitleCalls, struct{ ID, Title string }{id, title})
	return r.updateTitleErr
}
// Pin / Unpin — Phase 19 / D-02 atomic conditional updates (Plan 19-02 Task 1).
// Titler tests don't exercise pin lifecycle; stubs return nil.
func (r *titlerConvRepo) Pin(_ context.Context, _, _, _ string) error   { return nil }
func (r *titlerConvRepo) Unpin(_ context.Context, _, _, _ string) error { return nil }

func (r *titlerConvRepo) TransitionToAutoPending(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transitionCalls++
	if r.transitionErr != nil {
		return r.transitionErr
	}
	// Reflect successful transition in the stored conversation snapshot so
	// any subsequent caller observes the post-state. This lines up the fake
	// with the production atomic update semantics.
	if r.getByIDReturn != nil {
		r.getByIDReturn.TitleStatus = domain.TitleStatusAutoPending
	}
	return nil
}

// titlerMsgRepo is a focused fake for MessageRepository scoped to
// ListByConversationID. Other methods return safe defaults.
type titlerMsgRepo struct {
	listReturn []domain.Message
	listErr    error
}

func (m *titlerMsgRepo) Create(_ context.Context, _ *domain.Message) error { return nil }
func (m *titlerMsgRepo) ListByConversationID(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listReturn, nil
}
func (m *titlerMsgRepo) CountByConversationID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (m *titlerMsgRepo) Update(_ context.Context, _ *domain.Message) error { return nil }
func (m *titlerMsgRepo) FindByConversationActive(_ context.Context, _ string) (*domain.Message, error) {
	return nil, domain.ErrMessageNotFound
}

// newTitlerHandlerWithRealTitler builds a TitlerHandler with a REAL
// *service.Titler driven by service.FakeChatCaller — the canonical mocking
// seam introduced in Plan 04. NO parallel titlerCaller interface is used.
func newTitlerHandlerWithRealTitler(t *testing.T, conv *domain.Conversation, listMsgs []domain.Message) (*TitlerHandler, *titlerConvRepo, *service.FakeChatCaller) {
	t.Helper()
	convRepo := &titlerConvRepo{getByIDReturn: conv}
	msgRepo := &titlerMsgRepo{listReturn: listMsgs}
	fc := &service.FakeChatCaller{ReturnContent: "Запланировать пост"}
	titler := service.NewTitler(fc, convRepo, "test-model")
	h := NewTitlerHandler(titler, convRepo, msgRepo)
	return h, convRepo, fc
}

// TestRegenerateTitle_200_Success exercises the happy path: status=auto →
// 200 OK with no body; the atomic transition to auto_pending records on the
// repo; and the goroutine fires the FakeChatCaller (post-async settle).
func TestRegenerateTitle_200_Success(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439010"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  "biz-1",
		Title:       "Старый заголовок",
		TitleStatus: domain.TitleStatusAuto,
	}
	listMsgs := []domain.Message{
		{ID: "m1", ConversationID: convID, Role: "user", Content: "помоги"},
		{ID: "m2", ConversationID: convID, Role: "assistant", Content: "конечно", Status: domain.MessageStatusComplete},
	}
	h, convRepo, fc := newTitlerHandlerWithRealTitler(t, conv, listMsgs)

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Empty(t, rec.Body.String())
	assert.Equal(t, 1, convRepo.transitionCalls, "must atomically transition to auto_pending")

	// Goroutine settle. The titler is fire-and-forget; allow ~100ms for the
	// FakeChatCaller to record the call.
	deadline := time.Now().Add(500 * time.Millisecond)
	for fc.Calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, fc.Calls(), 1, "FakeChatCaller must observe at least one Chat invocation")
	require.NotNil(t, fc.LastReq(), "LastReq must be populated after the goroutine fired")
}

// TestRegenerateTitle_409_Manual asserts that status=manual returns 409 with
// the verbatim Russian copy from CONTEXT.md D-02.
func TestRegenerateTitle_409_Manual(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439020"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  "biz-1",
		TitleStatus: domain.TitleStatusManual,
	}
	h, convRepo, fc := newTitlerHandlerWithRealTitler(t, conv, nil)

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "title_is_manual", body["error"])
	// D-02 verbatim Russian copy locked in CONTEXT.md.
	assert.Equal(t, "Нельзя регенерировать — вы уже переименовали чат вручную", body["message"])

	// Negative assertions: no transition, no goroutine spawn.
	assert.Equal(t, 0, convRepo.transitionCalls)
	assert.Equal(t, 0, fc.Calls(), "manual must not fire the titler")
}

// TestRegenerateTitle_409_InFlight asserts that status=auto_pending returns
// 409 with the verbatim Russian copy from CONTEXT.md D-03.
func TestRegenerateTitle_409_InFlight(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439021"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  "biz-1",
		TitleStatus: domain.TitleStatusAutoPending,
	}
	h, convRepo, fc := newTitlerHandlerWithRealTitler(t, conv, nil)

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "title_in_flight", body["error"])
	// D-03 verbatim Russian copy locked in CONTEXT.md.
	assert.Equal(t, "Заголовок уже генерируется", body["message"])

	assert.Equal(t, 0, convRepo.transitionCalls)
	assert.Equal(t, 0, fc.Calls(), "in-flight must not fire a second titler call")
}

// TestRegenerateTitle_503_TitlerDisabled asserts the graceful-disable path
// (A6 / Pitfall 1): when the handler is constructed with titler=nil, the
// endpoint returns 503 with a structured body and never touches the repo.
func TestRegenerateTitle_503_TitlerDisabled(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439022"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  "biz-1",
		TitleStatus: domain.TitleStatusAuto,
	}
	convRepo := &titlerConvRepo{getByIDReturn: conv}
	msgRepo := &titlerMsgRepo{}
	h := NewTitlerHandler(nil, convRepo, msgRepo) // titler nil — graceful disable.

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "titler_disabled", body["error"])
	assert.NotEmpty(t, body["message"])
	assert.Equal(t, 0, convRepo.transitionCalls)
}

// TestRegenerateTitle_403_Forbidden asserts the ownership check: a different
// authenticated user cannot regenerate a conversation they do not own.
func TestRegenerateTitle_403_Forbidden(t *testing.T) {
	ownerID := uuid.New()
	attackerID := uuid.New()
	convID := "507f1f77bcf86cd799439023"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      ownerID.String(), // different user
		BusinessID:  "biz-1",
		TitleStatus: domain.TitleStatusAuto,
	}
	h, convRepo, fc := newTitlerHandlerWithRealTitler(t, conv, nil)

	rec := titlerRoute(t, h, attackerID, convID)

	require.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "forbidden", body["error"])
	assert.Equal(t, 0, convRepo.transitionCalls)
	assert.Equal(t, 0, fc.Calls())
}

// TestRegenerateTitle_404_NotFound asserts that GetByID returning
// ErrConversationNotFound surfaces as 404.
func TestRegenerateTitle_404_NotFound(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439024"
	convRepo := &titlerConvRepo{getByIDErr: domain.ErrConversationNotFound}
	msgRepo := &titlerMsgRepo{}
	fc := &service.FakeChatCaller{ReturnContent: "irrelevant"}
	titler := service.NewTitler(fc, convRepo, "test-model")
	h := NewTitlerHandler(titler, convRepo, msgRepo)

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusNotFound, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "conversation not found", body["error"])
	assert.Equal(t, 0, convRepo.transitionCalls)
	assert.Equal(t, 0, fc.Calls())
}

// TestRegenerateTitle_409_TransitionRace asserts the post-read race window:
// status was "auto" at the read (step 2), but TransitionToAutoPending returns
// ErrConversationNotFound because a manual rename arrived between read and
// atomic write. Surface as 409 "title_state_changed" so the frontend re-fetches.
func TestRegenerateTitle_409_TransitionRace(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439025"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  "biz-1",
		TitleStatus: domain.TitleStatusAuto,
	}
	convRepo := &titlerConvRepo{
		getByIDReturn: conv,
		transitionErr: domain.ErrConversationNotFound, // race-loss
	}
	msgRepo := &titlerMsgRepo{}
	fc := &service.FakeChatCaller{ReturnContent: "irrelevant"}
	titler := service.NewTitler(fc, convRepo, "test-model")
	h := NewTitlerHandler(titler, convRepo, msgRepo)

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "title_state_changed", body["error"])
	assert.NotEmpty(t, body["message"])
	// Negative: no goroutine should spawn after a transition-race loss.
	assert.Equal(t, 0, fc.Calls())
}

// TestRegenerateTitle_Unauthorized asserts that a request with no userID in
// the auth context is rejected with 401 before any repo lookup.
func TestRegenerateTitle_Unauthorized(t *testing.T) {
	convID := "507f1f77bcf86cd799439026"
	convRepo := &titlerConvRepo{}
	msgRepo := &titlerMsgRepo{}
	h := NewTitlerHandler(nil, convRepo, msgRepo)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/conversations/"+convID+"/regenerate-title", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.RegenerateTitle(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "unauthorized")
}

// TestRegenerateTitle_BodyVerbatimRussianCopy is a separate guard test that
// asserts the EXACT byte sequence of the two locked Russian 409 messages so
// any future copy-edit (e.g., a stray dash variant) fails loudly. CONTEXT.md
// D-02 / D-03 are byte-locked.
func TestRegenerateTitle_BodyVerbatimRussianCopy(t *testing.T) {
	cases := []struct {
		name        string
		titleStatus string
		wantError   string
		wantMessage string
	}{
		{
			name:        "manual",
			titleStatus: domain.TitleStatusManual,
			wantError:   "title_is_manual",
			wantMessage: "Нельзя регенерировать — вы уже переименовали чат вручную",
		},
		{
			name:        "in_flight",
			titleStatus: domain.TitleStatusAutoPending,
			wantError:   "title_in_flight",
			wantMessage: "Заголовок уже генерируется",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			userID := uuid.New()
			convID := "507f1f77bcf86cd799439027"
			conv := &domain.Conversation{
				ID: convID, UserID: userID.String(), BusinessID: "biz-1",
				TitleStatus: c.titleStatus,
			}
			h, _, _ := newTitlerHandlerWithRealTitler(t, conv, nil)

			rec := titlerRoute(t, h, userID, convID)

			require.Equal(t, http.StatusConflict, rec.Code)
			// Byte-exact comparison via strings.Contains so the test pinpoints
			// any copy drift (different dash, missing space) instantly.
			assert.True(t,
				strings.Contains(rec.Body.String(), c.wantMessage),
				"missing verbatim Russian copy %q in body %s",
				c.wantMessage, rec.Body.String())
			assert.True(t,
				strings.Contains(rec.Body.String(), c.wantError),
				"missing error code %q in body %s", c.wantError, rec.Body.String())
		})
	}
}

// Sanity check: a transition-error that is NOT ErrConversationNotFound
// surfaces as 500 (internal). Documents the alternative branch.
func TestRegenerateTitle_TransitionUnexpectedError_500(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439028"
	conv := &domain.Conversation{
		ID: convID, UserID: userID.String(), BusinessID: "biz-1",
		TitleStatus: domain.TitleStatusAuto,
	}
	convRepo := &titlerConvRepo{
		getByIDReturn: conv,
		transitionErr: errors.New("mongo: connection refused"),
	}
	msgRepo := &titlerMsgRepo{}
	fc := &service.FakeChatCaller{ReturnContent: "irrelevant"}
	titler := service.NewTitler(fc, convRepo, "test-model")
	h := NewTitlerHandler(titler, convRepo, msgRepo)

	rec := titlerRoute(t, h, userID, convID)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 0, fc.Calls())
}
