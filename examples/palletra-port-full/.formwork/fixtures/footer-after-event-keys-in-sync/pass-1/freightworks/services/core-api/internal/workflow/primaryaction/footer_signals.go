//go:build ignore

package primaryaction

// FooterSignal* is the closed after[event] key set on the Go side.
const (
	FooterSignalConfirm = "confirm"
	FooterSignalSkip    = "skip"
	FooterSignalRevise  = "revise"
)

// AllFooterActions re-lists the identifiers (no new string literals of its own).
func AllFooterActions() []string {
	return []string{FooterSignalConfirm, FooterSignalSkip, FooterSignalRevise}
}
