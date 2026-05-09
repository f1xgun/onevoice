// Package vkapi holds VK API constants shared between the API service
// (OAuth handler, group sync) and any future direct-call site.
//
// VK rotates its API version periodically; pinning the value here means
// every outbound URL pickups the bump in lockstep instead of drifting
// across files.
package vkapi

// APIVersion is the value of the &v= query param required by every VK
// REST method. See https://dev.vk.com/reference/versions.
const APIVersion = "5.199"

// DefaultAPIBaseURL is the production base URL for VK REST methods.
// Tests override this via cfg overrides without rewriting call sites.
const DefaultAPIBaseURL = "https://api.vk.com"

// DefaultOAuthBaseURL is the classic VK OAuth base (oauth.vk.com).
// The newer id.vk.com flow returns error 1051 ("method unavailable with
// current profile type") on groups.get and wall.getComments, so we pin
// the classic flow here.
const DefaultOAuthBaseURL = "https://oauth.vk.com"
