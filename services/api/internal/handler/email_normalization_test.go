package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestAuthEmailNormalizedBeforeValidation(t *testing.T) {
	for _, input := range []string{"OWNER@Example.COM", "  Owner@Example.com  "} {
		t.Run(input, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"`+input+`","password":"password123"}`))
			body, ok := decodeAndValidate[openapi.LoginRequest](httptest.NewRecorder(), req, "invalid")
			require.True(t, ok)
			require.Equal(t, "owner@example.com", string(body.Email))
			req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"`+input+`"}`))
			reset, ok := decodeAndValidate[openapi.RequestPasswordResetRequest](httptest.NewRecorder(), req, "invalid")
			require.True(t, ok)
			require.Equal(t, "owner@example.com", string(reset.Email))
			req = httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"newEmail":"`+input+`"}`))
			change, ok := decodeAndValidate[openapi.EmailBeforeVerifyRequest](httptest.NewRecorder(), req, "invalid")
			require.True(t, ok)
			require.Equal(t, "owner@example.com", string(change.NewEmail))
		})
	}
}
