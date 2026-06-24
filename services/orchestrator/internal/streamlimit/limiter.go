// Package streamlimit provides a process-wide concurrency cap for the
// orchestrator's SSE stream routes (POST /chat and /chat/{id}/resume).
//
// Each stream holds an LLM + NATS pipeline for the life of the request (up to
// the agent-loop budget), so without a global cap a burst of concurrent streams
// can exhaust the single orchestrator process. The api already enforces a
// per-user SSE cap (pkg/ssecounter); this is the aggregate backstop one tier
// below it: it bounds TOTAL concurrent streams regardless of which users they
// belong to, shedding load with a 503 rather than degrading every stream.
package streamlimit

import (
	"net/http"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// rejectedBody is the 503 payload, matching the orchestrator's other JSON error
// shapes (e.g. handler.Chat's `{"error":"..."}`).
const rejectedBody = `{"error":"stream_capacity_exceeded"}`

// Middleware returns a chi/net-http middleware that admits at most limit
// concurrent requests through next, rejecting the rest with 503. A single slot
// is held for the entire request (the whole SSE stream) and released when the
// handler returns. When limit <= 0 the cap is disabled and the middleware is a
// pass-through.
//
// Build ONE Middleware and apply it to every route that should share the cap —
// the returned function closes over a single semaphore, so reusing it across
// the chat and resume routes makes them share one global budget.
func Middleware(limit int) func(http.Handler) http.Handler {
	if limit <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	sem := make(chan struct{}, limit)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				metrics.OrchestratorStreamsInflight.Inc()
				defer func() {
					<-sem
					metrics.OrchestratorStreamsInflight.Dec()
				}()
				next.ServeHTTP(w, r)
			default:
				metrics.OrchestratorStreamsRejectedTotal.Inc()
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(rejectedBody))
			}
		})
	}
}
