package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Router provides a nice api around mux.Router.
type Router struct {
	r *mux.Router
}

// newRouter is a Mux HTTP router constructor.
func newRouter() *Router {
	r := mux.NewRouter()
	r.PathPrefix("/").Methods(http.MethodOptions) // accept OPTIONS on all routes and do nothing
	return &Router{r: r}
}

// get creates a subroute on the specified URI that only accepts GET. You can provide specific middlewares.
func (r *Router) get(uri string, f http.HandlerFunc, mid ...mux.MiddlewareFunc) { // nolint
	sub := r.r.Path(uri).Subrouter()
	sub.HandleFunc("", f).Methods(http.MethodGet)
	sub.Use(mid...)
}

// post creates a subroute on the specified URI that only accepts POST. You can provide specific middlewares.
func (r *Router) post(uri string, f http.HandlerFunc, mid ...mux.MiddlewareFunc) {
	sub := r.r.Path(uri).Subrouter()
	sub.HandleFunc("", f).Methods(http.MethodPost)
	sub.Use(mid...)
}

// use adds middlewares to all routes. Should be used when a middleware should be execute all all routes (e.g. CORS).
func (r *Router) use(mid ...mux.MiddlewareFunc) { // nolint
	r.r.Use(mid...)
}
