//go:build ignore

package taskview

import "github.com/palletra/freightworks/services/core-api/internal/projectterms"

// Correct: read the ordered taxonomy from the single source of truth rather
// than re-enumerating a parallel registry. The values below appear in a
// behavior switch, which the identifier-ban deliberately does not flag.
func firstState() projectterms.WorkflowPhase {
	return projectterms.WorkflowPhase.Values()[0]
}

func upcomingAction(s string) string {
	switch s {
	case "uploaded":
		return "price"
	case "priced":
		return "annotate"
	default:
		return "wait"
	}
}
