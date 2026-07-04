package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// decodeCapProbe is a tiny target struct for the decodeAndValidate body-cap
// test: one plain field, no validation tags, so only the body-size bound (not
// struct validation) can fail the decode.
type decodeCapProbe struct {
	Message string `json:"message"`
}

// TestDecodeAndValidate_OversizedBody_Rejected pins the shared body cap on the
// generic decode helper: a body over maxDecodeBodyBytes must fail the decode
// (ok == false) so every uncapped caller inherits the bound. The huge padding
// lives in an unknown field AFTER a valid one so MaxBytesReader fires while the
// stdlib decoder scans past the cap. Reverting the MaxBytesReader line lets the
// decoder buffer the whole body and return ok == true.
func TestDecodeAndValidate_OversizedBody_Rejected(t *testing.T) {
	padding := strings.Repeat("A", (2 << 20))
	body := `{"message":"x","_pad":"` + padding + `"}`

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()

	_, ok := decodeAndValidate[decodeCapProbe](w, req, "invalid_request")
	assert.False(t, ok, "over-cap body must be rejected")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDecodeAndValidate_NormalBody_Accepted is the control: a small valid body
// under the cap decodes cleanly (ok == true).
func TestDecodeAndValidate_NormalBody_Accepted(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"message":"hello"}`))
	w := httptest.NewRecorder()

	got, ok := decodeAndValidate[decodeCapProbe](w, req, "invalid_request")
	assert.True(t, ok, "normal body must be accepted: %s", w.Body.String())
	assert.Equal(t, "hello", got.Message)
}

// TestLogin_OversizedBody_Rejected_ServiceNotInvoked pins the body cap on an
// uncapped auth caller (Login goes through decodeAndValidate). An over-cap body
// is rejected (4xx) without ever calling the login service; a normal body still
// authenticates. Reverting the helpers.go MaxBytesReader line lets the huge body
// decode, reaching the service and flipping this to a 401 with Login invoked.
func TestLogin_OversizedBody_Rejected_ServiceNotInvoked(t *testing.T) {
	mockService := new(MockUserService)
	h, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

	padding := strings.Repeat("A", (2 << 20))
	body := `{"email":"user@example.com","password":"password123","_pad":"` + padding + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, req)

	assert.GreaterOrEqual(t, w.Code, http.StatusBadRequest)
	assert.Less(t, w.Code, http.StatusInternalServerError)
	mockService.AssertNotCalled(t, "Login")
}

// TestLogin_NormalBody_ReachesService is the control for the Login body-cap
// test: a small valid body decodes and reaches the login service.
func TestLogin_NormalBody_ReachesService(t *testing.T) {
	mockService := new(MockUserService)
	mockService.On("Login", mock.Anything, "user@example.com", "password123").
		Return(nil, "", "", domain.ErrInvalidCredentials)
	h, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

	body := `{"email":"user@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	mockService.AssertExpectations(t)
}
