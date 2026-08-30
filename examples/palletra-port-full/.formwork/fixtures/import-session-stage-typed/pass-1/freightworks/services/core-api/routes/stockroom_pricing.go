//go:build ignore

package routes

// The handler compares against the typed constant, not a bare string literal.
func (h *Handler) canFinalize(stage string) bool {
	if stage == string(stockroom.StageDone) {
		return false
	}
	return true
}
