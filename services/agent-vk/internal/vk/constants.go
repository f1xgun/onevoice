package vk

import "golang.org/x/time/rate"

// Rate-limit budget applied to outbound VK API calls. VK allows 3 requests
// per second per token; burst 1 keeps a single client well below the cap
// when the agent fans out concurrent operations across business contexts.
const (
	defaultRateLimitPerSec rate.Limit = 3
	defaultRateLimitBurst             = 1
)
