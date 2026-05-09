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

// APIVersion is the resolved &v= value used by every outbound VK URL.
// Initialized from VK_API_VERSION at process start, falling back to
// DefaultAPIVersion. Kept as a package-level var (not a const) so a
// single env nudge re-pins every call site simultaneously.
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
