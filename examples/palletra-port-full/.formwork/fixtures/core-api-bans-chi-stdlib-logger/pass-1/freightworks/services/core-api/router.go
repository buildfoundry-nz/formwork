//go:build ignore

package coreapi

import (
	"github.com/go-chi/chi/v5"
	apiMiddleware "github.com/palletra/freightworks/services/core-api/middleware"
)

// chi's stdlib middleware.Logger is banned (#3908) — it emits unstructured
// textPayload at DEFAULT severity. Use the slog RequestLog middleware instead.
func newBaseRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(apiMiddleware.RequestLog)
	return r
}
