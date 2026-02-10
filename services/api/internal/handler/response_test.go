package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestWriteValidationError(t *testing.T) {
	type testStruct struct {
		Email    string `validate:"required,email"`
		Password string `validate:"required,min=8"`
	}

	tests := []struct {
		name          string
		input         testStruct
		wantStatus    int
		checkResponse func(t *testing.T, body string)
	}{
		{
			name:       "missing required fields",
			input:      testStruct{},
			wantStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"fields"`)
				assert.Contains(t, body, `"Email"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
		{
			name: "invalid email format",
			input: testStruct{
				Email:    "not-an-email",
				Password: "password123",
			},
			wantStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name: "password too short",
			input: testStruct{
				Email:    "user@example.com",
				Password: "short",
			},
			wantStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
	}

	validate := validator.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			err := validate.Struct(tt.input)
			require.Error(t, err)

			writeValidationError(w, err)

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			tt.checkResponse(t, w.Body.String())
		})
	}
}
