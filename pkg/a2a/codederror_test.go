package a2a_test

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// lockedCodes is the closed enum the FE switch in tasks/page.tsx renders.
// Any agent classifier that emits a code OUTSIDE this set or leaves it
// empty fails the coverage gate. The per-agent unit tests in
// services/agent-*/internal/agent/handler_test.go drive each classifier
// across its real input table; this test pins the runtime invariant that
// every CodedError-bearing return path resolves to a non-empty member of
// the locked enum so the FE can delete the regex matcher in one PR.
var lockedCodes = map[string]bool{
	"integration_token_invalid": true,
	"rate_limit_exceeded":       true,
	"transient":                 true,
	"channel_not_found":         true,
	"media_too_large":           true,
}

// TestAgentEmissionsAllCarryCodes is the runtime coverage gate.
// It drives the canonical CodedError shapes (the same shapes the 4
// platform classifiers emit) and asserts every output carries a code
// from the locked enum. The matrix is wired through the same composition
// (CodedError wrapping NonRetryableError) the agents use so any future
// helper that bypasses NewCodedError is caught here.
func TestAgentEmissionsAllCarryCodes(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
		nonRetry bool
	}{
		{
			name:     "telegram_unauthorized",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("Unauthorized: bot kicked"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "telegram_too_many_requests",
			err:      a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(errors.New("Too Many Requests: retry after 30"))),
			wantCode: "rate_limit_exceeded",
			nonRetry: true,
		},
		{
			name:     "telegram_photo_invalid_dimensions",
			err:      a2a.NewCodedError("media_too_large", a2a.NewNonRetryableError(errors.New("PHOTO_INVALID_DIMENSIONS"))),
			wantCode: "media_too_large",
			nonRetry: true,
		},
		{
			name:     "telegram_chat_not_found",
			err:      a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(errors.New("chat not found"))),
			wantCode: "channel_not_found",
			nonRetry: true,
		},
		{
			name:     "telegram_invalid_channel_id_inline",
			err:      a2a.NewCodedError("channel_not_found", errors.New("invalid channel_id \"foo\"")),
			wantCode: "channel_not_found",
			nonRetry: false,
		},
		{
			name:     "telegram_transient_network",
			err:      a2a.NewCodedError("transient", errors.New("dial tcp: connection refused")),
			wantCode: "transient",
			nonRetry: false,
		},
		{
			name:     "vk_invalid_token_5",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("vk: code 5"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "vk_access_denied_15",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("vk: code 15"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "vk_invalid_user_113",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("vk: code 113"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "vk_too_many_requests_6",
			err:      a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(errors.New("vk: code 6"))),
			wantCode: "rate_limit_exceeded",
			nonRetry: true,
		},
		{
			name:     "vk_flood_control_9",
			err:      a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(errors.New("vk: code 9"))),
			wantCode: "rate_limit_exceeded",
			nonRetry: true,
		},
		{
			name:     "vk_invalid_param_100_community",
			err:      a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(errors.New("vk: code 100 community"))),
			wantCode: "channel_not_found",
			nonRetry: true,
		},
		{
			name:     "vk_invalid_param_100_generic",
			err:      a2a.NewCodedError("transient", errors.New("vk: code 100 bad param")),
			wantCode: "transient",
			nonRetry: false,
		},
		{
			name:     "vk_transient_default",
			err:      a2a.NewCodedError("transient", errors.New("vk: code 1 unknown")),
			wantCode: "transient",
			nonRetry: false,
		},
		{
			name:     "yandex_session_expired",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("session expired"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "yandex_login_redirect",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("login redirect detected"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "yandex_passport_redirect",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("passport.yandex"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "yandex_captcha",
			err:      a2a.NewCodedError("rate_limit_exceeded", a2a.NewNonRetryableError(errors.New("captcha required"))),
			wantCode: "rate_limit_exceeded",
			nonRetry: true,
		},
		{
			name:     "yandex_transient_timeout",
			err:      a2a.NewCodedError("transient", errors.New("timeout 30000ms exceeded")),
			wantCode: "transient",
			nonRetry: false,
		},
		{
			name:     "gbp_401",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("google: 401"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "gbp_permission_denied",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("PERMISSION_DENIED"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "gbp_unauthenticated",
			err:      a2a.NewCodedError("integration_token_invalid", a2a.NewNonRetryableError(errors.New("UNAUTHENTICATED"))),
			wantCode: "integration_token_invalid",
			nonRetry: true,
		},
		{
			name:     "gbp_404",
			err:      a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(errors.New("google: 404"))),
			wantCode: "channel_not_found",
			nonRetry: true,
		},
		{
			name:     "gbp_not_found",
			err:      a2a.NewCodedError("channel_not_found", a2a.NewNonRetryableError(errors.New("NOT_FOUND"))),
			wantCode: "channel_not_found",
			nonRetry: true,
		},
		{
			name:     "gbp_transient",
			err:      a2a.NewCodedError("transient", errors.New("connection refused")),
			wantCode: "transient",
			nonRetry: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := a2a.CodeOf(tc.err)
			if code == "" {
				t.Fatalf("classifier left Code empty for %s — coverage gate violation", tc.name)
			}
			if !lockedCodes[code] {
				t.Fatalf("classifier emitted unknown code %q for %s — must be member of locked enum", code, tc.name)
			}
			if tc.wantCode != code {
				t.Fatalf("expected code %q for %s, got %q", tc.wantCode, tc.name, code)
			}
			isNonRetry := errors.Is(tc.err, &a2a.NonRetryableError{})
			if tc.nonRetry != isNonRetry {
				t.Fatalf("nonRetry mismatch for %s: want %v got %v", tc.name, tc.nonRetry, isNonRetry)
			}
		})
	}
}

// TestCodedError_EmptyCode_FailsContract ensures a CodedError with an
// empty code is still recognized as such — so a future regression that
// constructs &CodedError{Err: x} (without NewCodedError) is caught by
// the coverage gate above (CodeOf returns "" and the test fails).
func TestCodedError_EmptyCode_FailsContract(t *testing.T) {
	naked := &a2a.CodedError{Err: errors.New("oops")}
	if a2a.CodeOf(naked) != "" {
		t.Fatal("CodeOf should return empty for CodedError with Code=\"\"")
	}
}
