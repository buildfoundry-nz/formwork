//go:build ignore

package middleware

import "net/http"

// FIRE: go.opentelemetry.io/otel is not imported.
func Tracing(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
