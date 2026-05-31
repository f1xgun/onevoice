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
