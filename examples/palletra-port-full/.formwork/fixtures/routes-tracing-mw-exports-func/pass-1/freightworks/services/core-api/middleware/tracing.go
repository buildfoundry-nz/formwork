//go:build ignore

package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel"
)

func Tracing(service string) func(http.Handler) http.Handler {
	tr := otel.Tracer(service)
	return func(next http.Handler) http.Handler {
		_ = tr
		return next
	}
}
