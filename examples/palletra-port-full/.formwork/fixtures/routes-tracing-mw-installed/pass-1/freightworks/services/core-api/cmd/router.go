//go:build ignore

package cmd

import (
	"github.com/go-chi/chi/v5"

	"example.com/core-api/middleware"
)

func makeRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Tracing("core-api"))
	return r
}
