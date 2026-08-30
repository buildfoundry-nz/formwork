//go:build ignore

package annotations

import "github.com/palletra/freightworks/services/core-api/internal/idempotency"

func mapErr(err error) int {
	if err == idempotency.ErrChecksumMismatch { // want: centralized-idempotency-error-mapping
		return 409
	}
	return 500
}
