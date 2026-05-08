package agent

// VK API error codes worth special-casing in classifyVKError. See
// https://dev.vk.com/reference/errors for the full list.
const (
	vkErrInvalidToken  = 5
	vkErrAccessDenied  = 15
	vkErrInvalidParam  = 100
	vkErrInvalidUser   = 113
	vkErrTooManyReqs   = 6
	vkErrFloodControl  = 9
)

// Default and max counts for read-only handler endpoints. The maximum
// matches VK's per-call ceiling (e.g. wall.get count=100); defaults are
// what the LLM gets when it omits the count field.
const (
	defaultCommentCount  = 20
	defaultWallPostCount = 10
	maxWallPostCount     = 100
)
