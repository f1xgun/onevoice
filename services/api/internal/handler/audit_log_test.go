package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// --- Test scaffolding ---

// fakeAuditLister is a hand-rolled stub for AuditLogLister that captures
// the inbound filter (so tests can verify filter threading) and returns a
// canned rows / err pair. Keeping it hand-rolled (vs testify/mock) makes
// the assertions on filter contents explicit at the test level.
type fakeAuditLister struct {
	gotBusinessID uuid.UUID
	gotFilter     domain.AuditLogFilter
	calls         int

	returnRows []repository.AuditLogRow
	returnErr  error
}

func (f *fakeAuditLister) ListByBusinessWithActors(_ context.Context, businessID uuid.UUID, filter domain.AuditLogFilter) ([]repository.AuditLogRow, error) {
	f.calls++
	f.gotBusinessID = businessID
	f.gotFilter = filter
	return f.returnRows, f.returnErr
}

// businessContextWithPerms wires a BusinessContext into ctx with explicit
// (businessID, userID, permissions). Mirrors the shape used by
// members_test.go / roles_test.go so the audit handler is exercised
// through the same RBAC plumbing the real router would attach.
func businessContextWithPerms(ctx context.Context, businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(ctx, authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		Permissions: perms,
	})
}

// makeRows builds n synthetic AuditLogRow values with timestamps spaced 1m
// apart (descending — newest first, matching ORDER BY created_at DESC).
// actorEmail of empty string produces a "failed-login" style row with no
// user_id. Otherwise it's an enriched LEFT JOIN row.
func makeRows(n int, biz uuid.UUID, actor *uuid.UUID, actorEmail string) []repository.AuditLogRow {
	rows := make([]repository.AuditLogRow, n)
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < n; i++ {
		rows[i] = repository.AuditLogRow{
			AuditLog: domain.AuditLog{
				ID:         uuid.New(),
				BusinessID: &biz,
				UserID:     actor,
				Action:     "rbac.role_granted",
				Resource:   "role",
				Details:    json.RawMessage(`{"target":"x"}`),
				CreatedAt:  base.Add(-time.Duration(i) * time.Minute),
			},
			ActorEmail:       actorEmail,
			ActorDisplayName: "",
		}
	}
	return rows
}

// --- List handler tests ---

// Happy path: owner with PermAuditRead gets 200 + enriched DTOs.
// Verifies (a) repo is called with the right business_id, (b) ActorEmail
// surfaces as a non-nil pointer in JSON, (c) action_category is derived
// from the action prefix, (d) NextCursor is null when len(rows) < limit.
func TestAuditLogHandler_List_HappyPath_EnrichesActor(t *testing.T) {
	t.Parallel()
	biz, actor := uuid.New(), uuid.New()
	rows := makeRows(3, biz, &actor, "viewer@test.local")

	stub := &fakeAuditLister{returnRows: rows}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?limit=50", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, biz, stub.gotBusinessID)
	require.Equal(t, 50, stub.gotFilter.Limit)

	var body AuditLogListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Items, 3)
	require.Nil(t, body.NextCursor, "page shorter than limit ⇒ no next cursor")
	require.Equal(t, "rbac.role_granted", string(body.Items[0].Action))
	require.Equal(t, "rbac", string(body.Items[0].ActionCategory))
	require.NotNil(t, body.Items[0].ActorEmail)
	require.Equal(t, "viewer@test.local", string(*body.Items[0].ActorEmail))
	require.Equal(t, actor, *body.Items[0].ActorId)
}

// Full-page response: len(rows) == limit ⇒ NextCursor is set and round-trips
// through audit.DecodeCursor. Verifies the cursor encodes the LAST row's
// (created_at, id) pair so the next request resumes at the right point.
func TestAuditLogHandler_List_FullPage_BuildsNextCursor(t *testing.T) {
	t.Parallel()
	biz, actor := uuid.New(), uuid.New()
	rows := makeRows(3, biz, &actor, "viewer@test.local")

	stub := &fakeAuditLister{returnRows: rows}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?limit=3", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body AuditLogListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotNil(t, body.NextCursor, "len(rows)==limit ⇒ next cursor present")

	gotT, gotID, err := audit.DecodeCursor(*body.NextCursor)
	require.NoError(t, err)
	last := rows[len(rows)-1]
	assert.Equal(t, last.ID, gotID)
	assert.Equal(t, last.CreatedAt.UTC(), gotT.UTC())
}

// Failed-login row: AuditLogRow.ActorEmail == "" ⇒ DTO.ActorEmail is nil
// ⇒ JSON emits "actor_email": null. Frontend renders
// "Неизвестен ({attempted_email})" by reading details.attempted_email.
func TestAuditLogHandler_List_NullActor_RendersNullEmail(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	rows := makeRows(1, biz, nil, "")
	rows[0].Action = "auth.login_failed"

	stub := &fakeAuditLister{returnRows: rows}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	items := raw["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	require.Nil(t, item["actor_email"], "failed-login row has null actor_email")
	require.Nil(t, item["actor_id"], "failed-login row has null actor_id")
	require.Equal(t, "auth", item["action_category"])
}

// Hard-deleted actor: the live LEFT JOIN email is empty (user_id SET NULL),
// but the write-time user_email_at_event snapshot preserves the forensic
// email for 152-ФЗ queries. The handler must fall back to the snapshot so the
// email is reachable via this endpoint.
func TestAuditLogHandler_List_HardDeletedActor_FallsBackToSnapshot(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	rows := makeRows(1, biz, nil, "")
	rows[0].Action = "user.self_deleted"
	rows[0].UserEmailAtEvent = "forensic@x"

	stub := &fakeAuditLister{returnRows: rows}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body AuditLogListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.NotNil(t, body.Items[0].ActorEmail, "snapshot email must surface after hard-delete")
	require.Equal(t, "forensic@x", string(*body.Items[0].ActorEmail))
}

// The live LEFT JOIN email wins over the snapshot when both are present —
// the fallback only fills the gap, it never overrides a live email.
func TestAuditLogHandler_List_LiveEmailWinsOverSnapshot(t *testing.T) {
	t.Parallel()
	biz, actor := uuid.New(), uuid.New()
	rows := makeRows(1, biz, &actor, "live@test.local")
	rows[0].UserEmailAtEvent = "stale@x"

	stub := &fakeAuditLister{returnRows: rows}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body AuditLogListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.NotNil(t, body.Items[0].ActorEmail)
	require.Equal(t, "live@test.local", string(*body.Items[0].ActorEmail))
}

// Default limit = 50 when ?limit= is absent.
func TestAuditLogHandler_List_DefaultLimit(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 50, stub.gotFilter.Limit)
}

// All filters thread through to the repository call: ?category, ?action,
// ?actor, ?from, ?to, ?cursor — each must end up on AuditLogFilter.
func TestAuditLogHandler_List_AllFiltersThreaded(t *testing.T) {
	t.Parallel()
	biz, actor := uuid.New(), uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	from := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	to := time.Now().UTC().Truncate(time.Second)
	cursorT := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	cursorID := uuid.New()
	cursor := audit.EncodeCursor(cursorT, cursorID)

	url := "/?category=rbac&action=rbac.role_granted" +
		"&actor=" + actor.String() +
		"&from=" + from.Format(time.RFC3339) +
		"&to=" + to.Format(time.RFC3339) +
		"&cursor=" + cursor +
		"&limit=10"

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	f := stub.gotFilter
	assert.Equal(t, "rbac", f.Category)
	assert.Equal(t, "rbac.role_granted", f.Action)
	require.NotNil(t, f.ActorID)
	assert.Equal(t, actor, *f.ActorID)
	require.NotNil(t, f.From)
	assert.True(t, f.From.Equal(from))
	require.NotNil(t, f.To)
	assert.True(t, f.To.Equal(to))
	require.NotNil(t, f.CursorTime)
	assert.True(t, f.CursorTime.Equal(cursorT))
	require.NotNil(t, f.CursorID)
	assert.Equal(t, cursorID, *f.CursorID)
	assert.Equal(t, 10, f.Limit)
}

// --- token_decrypted noise suppression ---

// By default (no ?action=) the handler excludes integration.token_decrypted —
// the once-per-action system event that would otherwise flood the journal.
func TestAuditLogHandler_List_DefaultHidesTokenDecrypted(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, stub.gotFilter.ExcludeActions, audit.ActionIntegrationTokenDecrypted,
		"default feed must exclude token_decrypted")
}

// A non-token_decrypted action filter (e.g. category=integration drilldown)
// still excludes token_decrypted, so the integration view stays clean.
func TestAuditLogHandler_List_OtherActionStillHidesTokenDecrypted(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?action=integration.connected", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "integration.connected", stub.gotFilter.Action)
	assert.Contains(t, stub.gotFilter.ExcludeActions, audit.ActionIntegrationTokenDecrypted)
}

// Explicitly selecting ?action=integration.token_decrypted is now a valid
// action (200, not 400) AND must NOT be excluded — this is how the user reveals
// the otherwise-hidden token-decrypt rows via the filter.
func TestAuditLogHandler_List_ExplicitTokenDecrypted_Revealed(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?action=integration.token_decrypted", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code, "token_decrypted must be an accepted action filter")
	assert.Equal(t, audit.ActionIntegrationTokenDecrypted, stub.gotFilter.Action)
	assert.NotContains(t, stub.gotFilter.ExcludeActions, audit.ActionIntegrationTokenDecrypted,
		"explicit token_decrypted filter must reveal those rows")
}

// integration.deleted is also a newly-accepted action filter (was 400 before).
func TestAuditLogHandler_List_IntegrationDeleted_Accepted(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?action=integration.deleted", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, audit.ActionIntegrationDeleted, stub.gotFilter.Action)
}

// --- Error paths ---

func TestAuditLogHandler_List_Forbidden_WithoutPerm(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
	require.Equal(t, 0, stub.calls, "repo MUST NOT be called when caller lacks perm")
}

func TestAuditLogHandler_List_MissingBusinessContext_500(t *testing.T) {
	t.Parallel()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, 0, stub.calls)
}

func TestAuditLogHandler_List_InvalidCursor_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?cursor=corrupt!", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_cursor")
	require.Equal(t, 0, stub.calls)
}

// Even a well-formed base64-of-not-JSON payload must map to 400, not 500.
func TestAuditLogHandler_List_InvalidCursor_BadJSON_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	bad := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte("not-json"))
	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?cursor="+bad, http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_cursor")
}

func TestAuditLogHandler_List_InvalidCategory_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?category=unknown", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_category")
	require.Equal(t, 0, stub.calls)
}

func TestAuditLogHandler_List_InvalidAction_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?action=not.a.real.action", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_action")
	require.Equal(t, 0, stub.calls)
}

func TestAuditLogHandler_List_InvalidActor_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?actor=not-a-uuid", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_actor")
}

func TestAuditLogHandler_List_InvalidDate_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/?from=yesterday", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_date")
}

func TestAuditLogHandler_List_InvalidLimit_400(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{}
	h := NewAuditLogHandler(stub)

	cases := []string{"0", "-1", "999", "abc"}
	for _, tc := range cases {
		ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
		req := httptest.NewRequest(http.MethodGet, "/?limit="+tc, http.NoBody).WithContext(ctx)
		w := httptest.NewRecorder()
		h.List(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, "limit=%q", tc)
		assertErrorCode(t, w, "invalid_limit")
	}
}

// Repo error → 500 + generic body. The raw error MUST NOT leak.
func TestAuditLogHandler_List_RepoError_500_NoLeak(t *testing.T) {
	t.Parallel()
	biz := uuid.New()
	stub := &fakeAuditLister{returnErr: errors.New("schema_secret_oops: column does not exist")}
	h := NewAuditLogHandler(stub)

	ctx := businessContextWithPerms(context.Background(), biz, uuid.New(), authz.PermAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assertErrorCode(t, w, "internal_server_error")
	require.NotContains(t, w.Body.String(), "schema_secret_oops")
}

// AuditLogLister assertion test — guards the wire-time type assertion in
// services/api/internal/wire/handlers.go. The concrete
// repository.NewAuditLogRepository constructor result MUST satisfy
// handler.AuditLogLister; if the repo ever drifts away from this shape
// the boot panic in wire would also fire here at test time.
func TestAuditLogHandler_RepoSatisfiesAuditLogLister(t *testing.T) {
	t.Parallel()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()
	repo := repository.NewAuditLogRepository(mockPool)
	_, ok := repo.(AuditLogLister)
	require.True(t, ok, "NewAuditLogRepository must return a value satisfying handler.AuditLogLister")
}
