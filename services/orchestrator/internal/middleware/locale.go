// Package middleware holds chi-compatible HTTP middlewares for the
// orchestrator service. Currently single-purpose (locale resolution);
// kept as a package so future additions (auth, tracing) get a natural
// home that mirrors `services/api/internal/middleware/`.
package middleware

import (
	"net/http"

	"github.com/f1xgun/onevoice/pkg/i18n"
)

// Locale parses the request's Accept-Language header, resolves it against
// pkg/i18n.Supported, and stores the chosen language.Tag in the request
// context via i18n.WithLocale. Downstream handlers — notably the chat
// handler that feeds the orchestrator's prompt builder (Phase D of
// `.planning/i18n-readiness/PLAN.md`) — read it via i18n.LocaleFromContext.
//
// Implementation mirrors `services/api/internal/middleware/locale.go`
// 1:1 so both services share the exact same Accept-Language semantics.
//
// On missing/malformed Accept-Language the resolver returns i18n.DefaultTag,
// so the wrapped handler always sees a non-zero Tag in context.
func Locale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := i18n.MatchAcceptLanguage(r.Header.Get("Accept-Language"))
		ctx := i18n.WithLocale(r.Context(), tag)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
