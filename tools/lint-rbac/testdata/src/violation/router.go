package violation

import (
	"net/http"
)

// Stub types for the fixture — the analyzer is pure AST, no type info needed.

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
		// No r.Use(authz.RequireBusinessAccess(...)) — missing chokepoint!

		r.Get("/foo", handlers.GetFoo)   // want `handler Get /foo registered under /businesses/\{id\}/\.\.\. must reference authz\.BusinessContextFromCtx or authz\.Can`
		r.Post("/bar", handlers.PostFoo) // want `handler Post /bar registered under /businesses/\{id\}/\.\.\. must reference authz\.BusinessContextFromCtx or authz\.Can`
	})
}
