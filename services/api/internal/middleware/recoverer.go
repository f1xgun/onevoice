package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// Recoverer returns a panic-recovery middleware. On a panic in any downstream
// handler it logs the panic and stack via slog.ErrorContext (so the
// correlation_id from the request context is attached), increments
// app_errors_total{service="api"} so a panicking handler is alertable, and
// responds 500 with the same machine-readable error envelope the handlers
// already emit for internal errors. It replaces chi's Recoverer, which neither
// records the metric nor carries the correlation_id.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				metrics.IncAppError(metrics.ServiceAPI)
				slog.ErrorContext(r.Context(), "recovered from panic",
					slog.Any("panic", rec),
					slog.String("method", r.Method),
					slog.String("path", redactInvitationToken(r.URL.Path)),
					slog.String("stack", string(debug.Stack())),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
