//go:build ignore

package middleware

import "net/http"

func Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query() // want: request-logger-excludes-sensitive-fields
		_ = q
		next.ServeHTTP(w, r)
	})
}
