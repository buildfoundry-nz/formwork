//go:build ignore

package bomfreshness

import "context"

// The lazy heal barrier is an approved seam for the project-wide eval, so a
// reference here is scoped out and must not fire.
func heal(ctx context.Context) error {
	return calceval.EvaluateProjectBomQuantities(ctx)
}
