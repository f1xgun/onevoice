package clean

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

type authzPkg struct{}

func (authzPkg) RequireBusinessAccess(cache interface{}, extractor interface{}) func(http.Handler) http.Handler {
	return nil
}

var authz = authzPkg{}

type handlersPkg struct {
	GetFoo http.HandlerFunc
	PostFoo http.HandlerFunc
}

var handlers = handlersPkg{}

func Setup(r Router) {
	r.Route("/businesses/{id}", func(r Router) {
		r.Use(authz.RequireBusinessAccess(nil, nil))

		r.Get("/foo", handlers.GetFoo)
		r.Post("/bar", handlers.PostFoo)
	})
}
