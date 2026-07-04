package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type fakeBillingSummarizer struct {
	summary service.BillingSummary
	err     error
	calls   int
	gotBiz  uuid.UUID
}

func (f *fakeBillingSummarizer) Summary(_ context.Context, businessID uuid.UUID) (service.BillingSummary, error) {
	f.calls++
	f.gotBiz = businessID
	return f.summary, f.err
}

func billingCtx(businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		Permissions: perms,
	})
}

func TestBillingHandler_Summary_HappyPath(t *testing.T) {
	biz := uuid.New()
	summary := service.BillingSummary{}
	summary.Plan.Code = "pro"
	stub := &fakeBillingSummarizer{summary: summary}
	h := NewBillingHandler(stub)

	ctx := billingCtx(biz, uuid.New(), authz.PermBillingRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Summary(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, biz, stub.gotBiz)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	plan := body["plan"].(map[string]any)
	require.Equal(t, "pro", plan["code"])
}

func TestBillingHandler_Summary_ForbiddenWithoutPermission(t *testing.T) {
	stub := &fakeBillingSummarizer{}
	h := NewBillingHandler(stub)

	// No PermBillingRead on the context.
	ctx := billingCtx(uuid.New(), uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Summary(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, 0, stub.calls, "handler must not call the service when authz fails")
}

func TestBillingHandler_Summary_InternalErrorOnServiceFailure(t *testing.T) {
	stub := &fakeBillingSummarizer{err: errors.New("db down")}
	h := NewBillingHandler(stub)

	ctx := billingCtx(uuid.New(), uuid.New(), authz.PermBillingRead)
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Summary(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}
