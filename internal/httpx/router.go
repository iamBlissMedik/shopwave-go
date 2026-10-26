package httpx

import (
	"net/http"
	"slices"
)

// Middleware wraps a handler (the standard net/http middleware shape).
type Middleware func(http.Handler) http.Handler

// Chain applies mw to h so that mw[0] is the outermost (runs first).
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Router is a thin layer over http.ServeMux (Go 1.22+ method + wildcard
// patterns) that adds path-prefix groups with shared middleware, like
// Express's `router.use(...)`.
type Router struct {
	mux    *http.ServeMux
	prefix string
	mw     []Middleware
}

func NewRouter(mux *http.ServeMux) *Router { return &Router{mux: mux} }

// Group returns a sub-router under prefix whose routes also run mw.
func (r *Router) Group(prefix string, mw ...Middleware) *Router {
	return &Router{mux: r.mux, prefix: r.prefix + prefix, mw: append(slices.Clone(r.mw), mw...)}
}

// Handle registers h for method + path (method "" matches any method), wrapped
// in the group's middleware and then any route-specific mw.
func (r *Router) Handle(method, path string, h HandlerFunc, mw ...Middleware) {
	pattern := r.prefix + path
	if method != "" {
		pattern = method + " " + pattern
	}
	all := append(slices.Clone(r.mw), mw...)
	r.mux.Handle(pattern, Chain(h, all...))
}

func (r *Router) Get(path string, h HandlerFunc, mw ...Middleware) {
	r.Handle(http.MethodGet, path, h, mw...)
}

func (r *Router) Post(path string, h HandlerFunc, mw ...Middleware) {
	r.Handle(http.MethodPost, path, h, mw...)
}

func (r *Router) Patch(path string, h HandlerFunc, mw ...Middleware) {
	r.Handle(http.MethodPatch, path, h, mw...)
}

func (r *Router) Delete(path string, h HandlerFunc, mw ...Middleware) {
	r.Handle(http.MethodDelete, path, h, mw...)
}
