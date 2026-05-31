package gbp

import (
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// HTTP client budget for outbound Google API calls. Google's GBP endpoints
// answer in <2s typically; 15s leaves headroom for cold paths without
// holding the agent's NATS reply for too long.
const defaultHTTPTimeout = 15 * time.Second

// Default Google Business Profile API hosts. Each is overridable per Client
// for tests so this file lists the production defaults; production callers
// never override.
const (
	defaultAccountsBaseURL     = "https://mybusinessaccountmanagement.googleapis.com"
	defaultBusinessInfoBaseURL = "https://mybusinessbusinessinformation.googleapis.com"
	defaultReviewsBaseURL      = "https://mybusiness.googleapis.com"
)

// Review pagination limits. Google's reviews endpoint caps page size at
// 50 (silently truncates if higher); DefaultReviewLimit is what the LLM
// gets when it omits the count field. Exported because the agent
// handler reuses it for the same default before calling client.GetReviews.
const (
	DefaultReviewLimit = domain.GoogleBusinessReviewLimitDefault
	maxReviewLimit     = 50
)
