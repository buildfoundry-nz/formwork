//go:build ignore

package middleware

import "net/http"

// spanStatus re-encodes the 5xx boundary instead of calling IsFailureStatus — a
// second source that can drift from the log-level mirror (sweep-19-#2).
func spanStatus(status int) string {
	if status >= http.StatusInternalServerError {
		return "error"
	}
	return "ok"
}
