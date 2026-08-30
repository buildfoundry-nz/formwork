//go:build ignore

package middleware

import "net/http"

// FIRE: go.opentelemetry.io/otel is not imported.
func Tracing(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// "go.opentelemetry.io/otel"
