//go:build ignore

package middleware

import "net/http"

// Auth is a plain chi middleware (auth only); it never touches idempotency.
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
