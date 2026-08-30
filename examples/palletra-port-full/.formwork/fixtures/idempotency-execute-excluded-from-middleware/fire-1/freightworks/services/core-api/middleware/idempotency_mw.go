//go:build ignore

package middleware

import (
	"net/http"

	"github.com/palletra/freightworks/services/core-api/internal/idempotency"
)

// Idempotency wrongly promotes the per-handler call site into chi middleware,
// fusing the two-tx contract into one tx and reopening the WI-101 deadlock.
func Idempotency(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = idempotency.Execute(r.Context(), tx, params, mutate) // want: idempotency-execute-excluded-from-middleware
		next.ServeHTTP(w, r)
	})
}
