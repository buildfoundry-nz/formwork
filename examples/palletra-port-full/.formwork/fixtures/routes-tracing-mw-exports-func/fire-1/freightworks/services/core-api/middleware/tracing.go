//go:build ignore

package middleware

import "net/http"

// FIRE: the chi span middleware was dropped from this file.
func passthrough(next http.Handler) http.Handler {
	return next
}
