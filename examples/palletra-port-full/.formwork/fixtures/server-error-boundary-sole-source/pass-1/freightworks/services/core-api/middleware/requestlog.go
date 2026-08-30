//go:build ignore

package middleware

import "net/http"

// IsFailureStatus is the single canonical encoding of the 5xx boundary.
func IsFailureStatus(status int) bool {
	return status >= http.StatusInternalServerError
}
