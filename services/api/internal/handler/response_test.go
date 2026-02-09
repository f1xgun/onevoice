package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		data       interface{}
		wantStatus int
		wantBody   string
	}{
		{
			name:       "success with map",
			status:     http.StatusOK,
			data:       map[string]string{"message": "success"},
			wantStatus: http.StatusOK,
			wantBody:   `{"message":"success"}`,
		},
		{
			name:       "created with struct",
			status:     http.StatusCreated,
			data:       struct{ ID string }{ID: "123"},
			wantStatus: http.StatusCreated,
			wantBody:   `{"ID":"123"}`,
		},
		{
			name:       "no content",
			status:     http.StatusNoContent,
			data:       nil,
			wantStatus: http.StatusNoContent,
			wantBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			writeJSON(w, tt.status, tt.data)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, w.Body.String())
			} else {
				assert.Empty(t, w.Body.String())
			}
		})
	}
}

func TestWriteJSONError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		message    string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "bad request",
			status:     http.StatusBadRequest,
			message:    "invalid input",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":"invalid input"}`,
		},
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			message:    "invalid credentials",
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"error":"invalid credentials"}`,
		},
		{
			name:       "internal server error",
			status:     http.StatusInternalServerError,
			message:    "internal server error",
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			writeJSONError(w, tt.status, tt.message)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.JSONEq(t, tt.wantBody, w.Body.String())
		})
	}
}
