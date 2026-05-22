package middleware

import (
	"net/http"

	"github.com/f1xgun/onevoice/pkg/i18n"
)

// Locale parses the request's Accept-Language header, resolves it against
// pkg/i18n.Supported, and stores the chosen language.Tag in the request
// context via i18n.WithLocale. Downstream handlers read it via
// i18n.LocaleFromContext or i18n.Tr.
//
// Mirrors the no-arg pattern of correlation_id-style middleware: zero
// configuration, safe to chain anywhere after CORS/correlation but before
// auth so even unauthenticated error responses can be localized.
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
