// Package vkapi holds VK API constants shared between the API service
// (OAuth handler, group sync) and any future direct-call site.
//
// VK rotates its API version periodically; pinning the value here means
// every outbound URL pickups the bump in lockstep instead of drifting
// across files.
package vkapi

import "os"

// DefaultAPIVersion is the &v= query param baked into the binary. Live
// API version is resolved through APIVersion (variable below) so the
// VK_API_VERSION env var can override it without a redeploy when VK
// announces a new version. See https://dev.vk.com/reference/versions.
const DefaultAPIVersion = "5.199"

// APIVersion is the resolved &v= value for outbound VK URLs. Initialized
// from VK_API_VERSION at process start, falling back to DefaultAPIVersion.
var APIVersion = resolveAPIVersion()

func resolveAPIVersion() string {
	if v := os.Getenv("VK_API_VERSION"); v != "" {
		return v
	}
	return DefaultAPIVersion
}

// DefaultAPIBaseURL is the production base URL for VK REST methods.
// Tests override this via cfg overrides without rewriting call sites.
const DefaultAPIBaseURL = "https://api.vk.com"

// DefaultOAuthBaseURL is the classic VK OAuth base (oauth.vk.com).
// The newer id.vk.com flow returns error 1051 ("method unavailable with
// current profile type") on groups.get and wall.getComments, so we pin
// the classic flow here.
const DefaultOAuthBaseURL = "https://oauth.vk.com"

// URLPrefixes is the set of URL fragments stripped off user-pasted group
// inputs (e.g. "https://vk.com/mygroup" → "mygroup"). These are NOT
// endpoints we request — they are pattern strings for prefix matching
// shared by the OAuth and Connect handlers.
var URLPrefixes = []string{"https://vk.com/", "http://vk.com/", "https://m.vk.com/", "vk.com/", "@"}

// VK API error codes that must be classified consistently everywhere a VK
// response envelope is inspected. See https://dev.vk.com/reference/errors.
//
// The auth codes are genuine, conclusive credential failures; the transient
// codes are rate-limit / anti-bot signals that must fail soft (never demote a
// working integration to broken). Shared here so the API health probes and the
// agent-vk error classifier agree on the same code semantics.
const (
	// ErrCodeInvalidToken (5): the access token is invalid/expired.
	ErrCodeInvalidToken = 5
	// ErrCodeAccessDenied (15): the token cannot act on the target object.
	ErrCodeAccessDenied = 15
	// ErrCodeInvalidUser (113): invalid user/community id for the token.
	ErrCodeInvalidUser = 113

	// ErrCodeTooManyRequests (6): too many requests per second.
	ErrCodeTooManyRequests = 6
	// ErrCodeFloodControl (9): flood control triggered.
	ErrCodeFloodControl = 9
	// ErrCodeCaptchaNeeded (14): a captcha challenge is required.
	ErrCodeCaptchaNeeded = 14
)

// IsAuthErrorCode reports whether a VK error code is a conclusive credential
// failure (invalid/expired token, access denied, invalid user/community). Only
// these codes justify demoting an integration to broken; every other envelope
// (rate-limit, flood, captcha, or an unrecognized transient error) must fail
// soft to an inconclusive verdict.
func IsAuthErrorCode(code int) bool {
	switch code {
	case ErrCodeInvalidToken, ErrCodeAccessDenied, ErrCodeInvalidUser:
		return true
	default:
		return false
	}
}
