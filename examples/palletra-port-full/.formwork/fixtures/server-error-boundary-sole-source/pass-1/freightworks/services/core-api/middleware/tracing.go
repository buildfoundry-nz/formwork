//go:build ignore

package middleware

// spanStatus routes the 5xx decision through the single canonical source,
// IsFailureStatus — it never re-encodes the boundary (sweep-19-#2).
func spanStatus(status int) string {
	if IsFailureStatus(status) {
		return "error"
	}
	return "ok"
}
