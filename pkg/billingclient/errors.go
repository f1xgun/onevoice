package billingclient

import "errors"

// Sentinel errors returned by Client.LogUsage so callers can branch on
// retryability without string-matching the wrapped error.
//
// The orchestrator's pkg/llm.Router calls LogUsage from a fire-and-forget
// goroutine in `logBilling` — the caller logs and drops the error. Sentinels
// are still chained via fmt.Errorf("%w: ...") for two reasons:
//
//  1. The Prometheus counter `llm_billing_post_failures_total{reason}` is
//     labeled by the same two buckets — symmetry between the wire error and
//     the metric means a Grafana panel can be cross-referenced against an
//     errors.Is(err, ErrTransient) log filter without ambiguity.
//
//  2. A future router-side retry policy can match the tokenclient
//     precedent (ErrTransient retryable, others terminal) so a single
//     router-wide retry policy applies uniformly across both clients
//     without per-client logic.
//
// Sentinel mapping (mirrored in client.go):
//
//   - ErrTransient — network failure (DNS / connection refused / TLS hiccup)
//     OR HTTP 5xx response. Retrying may succeed.
//
//   - ErrInvalidPayload — local validation failure (nil log, missing
//     BusinessID, marshal error) OR HTTP 400 from the API handler. The
//     payload itself is malformed; retrying verbatim will not help.
//
// Unexpected non-2xx, non-4xx, non-5xx responses (e.g. a stray 418) surface
// as a plain error WITHOUT a sentinel — matching tokenclient's
// "TestGetToken_UnexpectedNon5xx_NoSentinel" contract. The counter records
// these under reason="unexpected_status" so operators can spot misconfigured
// reverse proxies in front of the API.
var (
	// ErrTransient — network failure / 5xx response. Caller may safely retry.
	ErrTransient = errors.New("billingclient: transient API failure")
	// ErrInvalidPayload — local validation failure or 400. Caller MUST NOT retry verbatim.
	ErrInvalidPayload = errors.New("billingclient: invalid payload")
)
