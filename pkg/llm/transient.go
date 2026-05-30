package llm

import (
	"errors"
	"net"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/sashabaranov/go-openai"
)

// isTransientLLMError reports whether a provider error is a candidate for a
// one-shot retry against a sibling registry entry. Transient classes:
//
//   - net.Error with Timeout()=true or Temporary()=true (in-flight network
//     blip; the next dial may succeed)
//   - typed SDK APIError carrying HTTP 429 (rate limited; a sibling provider
//     has its own bucket)
//   - typed SDK APIError carrying HTTP 5xx (upstream outage; sibling provider
//     is on a different upstream)
//
// 4xx other than 429 means the caller is at fault (bad request, invalid
// model, missing auth) — retrying against a sibling cannot help.
//
// The substring fallback catches errors whose typed shape we don't recognize
// (e.g. raw HTTP transport errors that never get wrapped in an APIError, or
// future SDK versions whose error type names drift). It is intentionally
// conservative: 429 / 502 / 503 / 504 are the only tokens checked because
// they are unambiguous status codes.
func isTransientLLMError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	// Temporary() is deprecated upstream but remains the only signal for
	// non-timeout transient transport errors (connection reset, refused).
	// Keeping it preserves coverage of those classes without forcing a
	// substring-based fallback.
	//
	//nolint:staticcheck // SA1019: net.Error.Temporary() still useful for retry classification.
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	var oaErr *openai.APIError
	if errors.As(err, &oaErr) {
		return oaErr.HTTPStatusCode == http.StatusTooManyRequests ||
			(oaErr.HTTPStatusCode >= 500 && oaErr.HTTPStatusCode < 600)
	}
	var anthErr *anthropic.Error
	if errors.As(err, &anthErr) {
		return anthErr.StatusCode == http.StatusTooManyRequests ||
			(anthErr.StatusCode >= 500 && anthErr.StatusCode < 600)
	}
	msg := err.Error()
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "504")
}
