package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

type fakeFeedbackSubmitter struct {
	called  int
	gotUser uuid.UUID
	gotIn   service.FeedbackInput
	err     error
}

func (f *fakeFeedbackSubmitter) Submit(_ context.Context, userID uuid.UUID, in service.FeedbackInput) error {
	f.called++
	f.gotUser = userID
	f.gotIn = in
	return f.err
}

func postFeedback(t *testing.T, h *FeedbackHandler, body string, userID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feedback", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if userID != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, *userID))
	}
	w := httptest.NewRecorder()
	h.Submit(w, req)
	return w
}

func TestFeedbackHandler_Submit_Valid(t *testing.T) {
	fake := &fakeFeedbackSubmitter{}
	h := NewFeedbackHandler(fake)
	uid := uuid.New()
	rating := 5
	body, _ := json.Marshal(map[string]any{"category": "idea", "message": "add dark mode", "page": "/chat", "rating": rating})

	w := postFeedback(t, h, string(body), &uid)

	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, 1, fake.called)
	assert.Equal(t, uid, fake.gotUser)
	assert.Equal(t, "idea", fake.gotIn.Category)
	require.NotNil(t, fake.gotIn.Rating)
	assert.Equal(t, int16(5), *fake.gotIn.Rating)
}

func TestFeedbackHandler_Submit_Unauthorized(t *testing.T) {
	fake := &fakeFeedbackSubmitter{}
	h := NewFeedbackHandler(fake)
	body, _ := json.Marshal(map[string]any{"category": "bug", "message": "x"})

	w := postFeedback(t, h, string(body), nil)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Zero(t, fake.called)
}

func TestFeedbackHandler_Submit_InvalidCategory(t *testing.T) {
	fake := &fakeFeedbackSubmitter{}
	h := NewFeedbackHandler(fake)
	uid := uuid.New()
	body, _ := json.Marshal(map[string]any{"category": "spam", "message": "x"})

	w := postFeedback(t, h, string(body), &uid)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, fake.called)
}

func TestFeedbackHandler_Submit_EmptyMessage(t *testing.T) {
	fake := &fakeFeedbackSubmitter{}
	h := NewFeedbackHandler(fake)
	uid := uuid.New()
	body, _ := json.Marshal(map[string]any{"category": "bug", "message": ""})

	w := postFeedback(t, h, string(body), &uid)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, fake.called)
}

func TestFeedbackHandler_Submit_ServiceError(t *testing.T) {
	fake := &fakeFeedbackSubmitter{err: errors.New("db down")}
	h := NewFeedbackHandler(fake)
	uid := uuid.New()
	body, _ := json.Marshal(map[string]any{"category": "bug", "message": "broken"})

	w := postFeedback(t, h, string(body), &uid)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
