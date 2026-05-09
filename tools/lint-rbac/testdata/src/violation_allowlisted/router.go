package violation_allowlisted

import (
	"net/http"
)

// Same shape as testdata/src/violation/router.go but with no diagnostic
// expectations: TestAnalyzer_AllowlistSuppressesDiagnostics seeds
// activeAllowlist so the analyzer suppresses both diagnostics.

type Router interface {
	Use(middlewares ...func(http.Handler) http.Handler)
	Get(pattern string, handlerFn http.HandlerFunc)
	Post(pattern string, handlerFn http.HandlerFunc)
	Route(pattern string, fn func(r Router))
}

type handlersPkg struct {
	GetFoo  http.HandlerFunc
	PostFoo http.HandlerFunc
}

var handlers = handlersPkg{}

func Setup(r Router) {
	r.Route("/businesses/{id}", func(r Router) {
		// No r.Use(authz.RequireBusinessAccess(...)) — would normally diagnose,
		// but both routes below are in the test's activeAllowlist.
		r.Get("/foo", handlers.GetFoo)
		r.Post("/bar", handlers.PostFoo)
	})
}
