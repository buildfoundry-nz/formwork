//go:build ignore

package intakeflow

import (
	"context"
	"time"
)

// ProcessIncomingSubmission opens ONE function-scope timeout and shares it across
// every attachment, so a slow early PDF starves the tail (sweep-12 #4 / #7280).
func ProcessIncomingSubmission(pdfFiles []string) []error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // want: per-pdf-timeout-budget-inside-loop
	defer cancel()
	var failed []error
	for _, att := range pdfFiles {
		if _, err := processOnePDF(ctx, att); err != nil {
			failed = append(failed, err)
		}
	}
	return failed
}
