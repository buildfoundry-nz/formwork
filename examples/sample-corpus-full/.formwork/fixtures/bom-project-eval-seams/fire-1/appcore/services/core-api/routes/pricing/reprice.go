//go:build ignore

package pricing

import "context"

// reprice open-codes a project-wide BOM eval outside the approved seams.
func reprice(ctx context.Context) error {
	return calceval.EvaluateProjectBomQuantities(ctx) // want: bom-project-eval-seams
}
