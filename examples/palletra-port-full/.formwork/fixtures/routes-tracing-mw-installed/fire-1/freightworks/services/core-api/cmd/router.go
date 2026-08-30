//go:build ignore

package cmd

import "github.com/go-chi/chi/v5"

// FIRE: the router never installs the Tracing middleware.
func makeRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(logger)
	return r
}
