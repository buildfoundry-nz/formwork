//go:build ignore

package middleware

import "net/http"

// FIRE: the chi span middleware was dropped from this file.
func passthrough(next http.Handler) http.Handler {
	return next
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func Tracing(service string) func(http.Handler) http.Handler {
