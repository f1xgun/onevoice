package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
)

// TestRegenerateTitle_409_Manual_EnglishLocale verifies that the RU 409
// "manual rename" message is rendered in English when the request carries
// an English locale on its context. The locale would normally be planted
// by middleware.Locale parsing Accept-Language; this test plants it
// directly to keep the handler-level assertion focused on the i18n
// resolution path.
//
// Mirrors TestRegenerateTitle_409_Manual but with Accept-Language: en.
func TestRegenerateTitle_409_Manual_EnglishLocale(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439040"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  titlerBiz.String(),
		TitleStatus: domain.TitleStatusManual,
	}
	h, _, _ := newTitlerHandlerWithRealTitler(t, conv, nil)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/conversations/"+convID+"/regenerate-title", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := titlerBizCtx(titlerBiz, userID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	ctx = i18n.WithLocale(ctx, language.English)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.RegenerateTitle(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "title_is_manual", body["error"])
	assert.Equal(t,
		"Can't regenerate — you've already renamed this chat manually",
		body["message"])
}

// TestRegenerateTitle_409_InFlight_EnglishLocale mirrors the in-flight
// 409 path under the EN locale.
func TestRegenerateTitle_409_InFlight_EnglishLocale(t *testing.T) {
	userID := uuid.New()
	convID := "507f1f77bcf86cd799439041"
	conv := &domain.Conversation{
		ID:          convID,
		UserID:      userID.String(),
		BusinessID:  titlerBiz.String(),
		TitleStatus: domain.TitleStatusAutoPending,
		UpdatedAt:   time.Now(),
	}
	h, _, _ := newTitlerHandlerWithRealTitler(t, conv, nil)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/conversations/"+convID+"/regenerate-title", http.NoBody)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	ctx := titlerBizCtx(titlerBiz, userID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	ctx = i18n.WithLocale(ctx, language.English)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.RegenerateTitle(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "title_in_flight", body["error"])
	assert.Equal(t, "Title is already being generated", body["message"])
}

// TestWriteJSONErrorKey_DefaultLocaleIsRussian verifies the writeJSONErrorKey
// helper falls back to the RU catalog when no locale is on the context (the
// production middleware always plants one; this test guards against the
// no-middleware path used by some legacy tests).
func TestWriteJSONErrorKey_DefaultLocaleIsRussian(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	writeJSONErrorKey(w, r, http.StatusBadRequest, "api.title.conflict.manual_rename")

	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t,
		"Нельзя регенерировать — вы уже переименовали чат вручную",
		body["error"])
}

// TestWriteJSONErrorKey_RespectsEnglishLocale confirms the helper picks
// the EN catalog when the locale is explicitly EN on context — covers the
// path Accept-Language: en would take after middleware.Locale runs.
func TestWriteJSONErrorKey_RespectsEnglishLocale(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r = r.WithContext(i18n.WithLocale(r.Context(), language.English))

	writeJSONErrorKey(w, r, http.StatusBadRequest, "api.title.conflict.manual_rename")

	require.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t,
		"Can't regenerate — you've already renamed this chat manually",
		body["error"])
}
