//go:build ignore

package routes

// A bare-string stage comparison in a handler — forbidden; it re-implements the
// enum string set inline instead of using the typed stockroom.Stage constants.
func (h *Handler) canFinalize(stage string) bool {
	if stage == "complete" { // want: import-session-stage-typed
		return false
	}
	return true
}
