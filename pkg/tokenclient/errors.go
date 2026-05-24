package tokenclient

import "errors"

// Sentinel errors returned by Client.GetToken so callers can branch on
// retryability without string-matching.
//
// Pre-sentinel-seam, the four agent handlers
// (services/agent-{telegram,vk,yandex-business,google-business}/internal/agent/handler.go)
// all wrapped any token-fetch failure as *a2a.NonRetryableError —
// blanket-permanent even for transient 5xx / network blips. That collapses
// two distinct outcomes (the integration is gone vs the network is flaky)
// into one classification.
//
// The three sentinels below let callers distinguish them:
//
//   - ErrIntegrationNotFound — the API returned 404. The integration was
//     never registered, was deleted, or the (businessID, platform,
//     externalID) triple does not resolve to any active row. Permanent
//     until the user reconnects the platform.
//
//   - ErrTokenExpired — the API returned 410 Gone. The stored credential
//     is past its expires_at AND the refresher attempt failed (or no
//     refresher is configured for the platform). Permanent until the
//     user re-authenticates.
//
//   - ErrTransient — the underlying HTTP call failed (network / DNS /
//     connection refused) OR the API returned a 5xx status. Retrying
//     the same token fetch may succeed.
//
// Errors returned by GetToken chain the sentinel via fmt.Errorf("%w: ...")
// so errors.Is(err, ErrIntegrationNotFound) etc. resolves correctly even
// after additional wrapping at the call site.
//
// The error MESSAGES are kept byte-identical to the pre-sentinel strings
// ("tokenclient: integration not found", etc.) so log greps in production
// dashboards do not break.
var (
	ErrIntegrationNotFound = errors.New("tokenclient: integration not found")
	ErrTokenExpired        = errors.New("tokenclient: token expired and refresh failed")
	ErrTransient           = errors.New("tokenclient: transient API failure")
)
