//go:build ignore

package cmd

import "github.com/go-chi/chi/v5"

// FIRE: the router never installs the Tracing middleware.
func makeRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(logger)
	return r
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// r.Use(middleware.Tracing("core-api"))
