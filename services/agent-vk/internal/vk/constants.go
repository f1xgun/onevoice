package vk

import "golang.org/x/time/rate"

// Rate-limit budget applied to outbound VK API calls. VK allows 3 requests
// per second per token; burst 1 keeps a single client well below the cap
// when the agent fans out concurrent operations across business contexts.
//
// Split into two const declarations because grouping them under a single
// `const ( ... )` block makes staticcheck SA9004 (implicit type
// inheritance) flag defaultRateLimitBurst as silently typed rate.Limit,
// which it isn't (golang.org/x/time/rate.NewLimiter takes an int burst).
const defaultRateLimitPerSec rate.Limit = 3
const defaultRateLimitBurst = 1
