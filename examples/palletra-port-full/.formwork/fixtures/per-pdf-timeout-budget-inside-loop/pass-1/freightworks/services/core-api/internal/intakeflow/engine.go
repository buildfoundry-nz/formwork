//go:build ignore

package intakeflow

import (
	"context"
	"time"
)

// ProcessIncomingSubmission gives each attachment its OWN per-PDF budget created
// inside the loop, so one slow PDF cannot starve the rest (sweep-12 #4).
func ProcessIncomingSubmission(pdfFiles []string) []error {
	var failed []error
	for _, att := range pdfFiles {
		docCtx, docCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if _, err := processOnePDF(docCtx, att); err != nil {
			failed = append(failed, err)
		}
		docCancel()
	}
	return failed
}
