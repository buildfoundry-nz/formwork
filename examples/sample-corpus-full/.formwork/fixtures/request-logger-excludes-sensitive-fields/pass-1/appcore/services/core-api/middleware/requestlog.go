//go:build ignore

package middleware

import (
	"log/slog"
	"net/http"
)

func Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "req", "method", r.Method, "path", r.URL.Path, "addr", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
