package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
)

type registrationVerifyStub struct {
	EmailVerificationServiceAPI
	called bool
	email  string
}

func (s *registrationVerifyStub) ChangeEmailBeforeVerify(_ context.Context, _ uuid.UUID, email string) (string, error) {
	s.called = true
	s.email = email
	return "invited@example.org", nil
}

func TestRegister_InviteOnlyMissingGateFailsClosed(t *testing.T) {
	users := new(MockUserService)
	h, err := NewAuthHandler(users, false, audit.Nop(), testJWTSecret)
	require.NoError(t, err)
	h.WithInviteOnlyRegistration(nil)
	body := `{"email":"owner@example.org","name":"Owner","password":"password123",` + registerConsentsJSON(legalconfig.PDNVersion) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.Register(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.JSONEq(t, `{"code":"registration_gate_unavailable"}`, w.Body.String())
	users.AssertNumberOfCalls(t, "RegisterWithContext", 0)
}

func TestEmailBeforeVerify_RegistrationAccess(t *testing.T) {
	for _, tc := range []struct {
		name       string
		inviteOnly bool
		gate       *stubRegistrationAccessGate
		wantStatus int
		wantCode   string
	}{
		{name: "open registration preserves email changes", wantStatus: http.StatusNoContent},
		{name: "unapproved address cannot bypass registration", inviteOnly: true, gate: &stubRegistrationAccessGate{}, wantStatus: http.StatusForbidden, wantCode: "registration_invite_required"},
		{name: "missing gate fails closed", inviteOnly: true, wantStatus: http.StatusServiceUnavailable, wantCode: "registration_gate_unavailable"},
		{name: "store failure fails closed", inviteOnly: true, gate: &stubRegistrationAccessGate{err: context.DeadlineExceeded}, wantStatus: http.StatusServiceUnavailable, wantCode: "registration_gate_unavailable"},
		{name: "approved correction keeps normal flow", inviteOnly: true, gate: &stubRegistrationAccessGate{allowed: true}, wantStatus: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewAuthHandler(new(MockUserService), false, audit.Nop(), testJWTSecret)
			require.NoError(t, err)
			verifier := &registrationVerifyStub{}
			h.SetEmailVerificationService(verifier)
			if tc.inviteOnly {
				if tc.gate != nil {
					h.WithInviteOnlyRegistration(tc.gate)
				} else {
					h.WithInviteOnlyRegistration(nil)
				}
			}
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/email-before-verify", strings.NewReader(`{"newEmail":"  Changed@Example.ORG  "}`))
			req = ctxWithUserID(req, uuid.New())
			w := httptest.NewRecorder()
			h.EmailBeforeVerify(w, req)
			require.Equal(t, tc.wantStatus, w.Code)
			require.Equal(t, tc.wantStatus == http.StatusNoContent, verifier.called)
			if tc.gate != nil {
				require.Equal(t, "changed@example.org", tc.gate.email)
			}
			if tc.wantCode != "" {
				require.JSONEq(t, `{"code":"`+tc.wantCode+`"}`, w.Body.String())
			} else {
				require.Equal(t, "changed@example.org", verifier.email)
			}
		})
	}
}
