package middleware

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/logger"
)

const correlationIDHeader = "X-Correlation-ID"

// CorrelationID returns middleware that extracts or generates a correlation ID
// for every request. The ID is stored in context via logger.WithCorrelationID
// and echoed back in the X-Correlation-ID response header.
func CorrelationID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(correlationIDHeader)
			if id == "" {
				id = uuid.New().String()
			}

			ctx := logger.WithCorrelationID(r.Context(), id)
			w.Header().Set(correlationIDHeader, id)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
