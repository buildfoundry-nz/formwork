//go:build ignore

package middleware

import "net/http"

func ObservabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), "http.server") // want: inbound-trace-extraction
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
