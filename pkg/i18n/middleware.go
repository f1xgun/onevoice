package i18n

import "net/http"

// LocaleMiddleware parses the request's Accept-Language header, resolves it
// against Supported via MatchAcceptLanguage, and stores the chosen
// language.Tag in the request context via WithLocale. Downstream handlers
// read it via LocaleFromContext or Tr.
//
// Both the api and orchestrator services mount this exactly once via
// chi.Router.Use so the locale flows uniformly from HTTP edge to handler,
// service, and prompt-builder layers. Sharing the implementation means a
// future change to Accept-Language semantics (e.g. cookie fallback) lives
// in one place — the deletion test: removing this function reintroduces
// near-identical 3-line stubs in every service.
//
// On missing/malformed Accept-Language the resolver returns DefaultTag, so
// the wrapped handler always sees a non-zero Tag in context. Mounting order
// — after CORS / correlation, before auth — matches the existing
// per-service usage so even unauthenticated error responses can be
// localized.
func LocaleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := MatchAcceptLanguage(r.Header.Get("Accept-Language"))
		ctx := WithLocale(r.Context(), tag)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
