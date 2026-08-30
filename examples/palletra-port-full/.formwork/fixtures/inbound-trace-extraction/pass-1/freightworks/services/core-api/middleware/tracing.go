//go:build ignore

package middleware

import "net/http"

func ObservabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		carrier := propagation.HeaderCarrier(r.Header)
		parent := otel.GetTextMapPropagator().Extract(r.Context(), carrier)
		ctx, span := tracer.Start(parent, "http.server")
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
