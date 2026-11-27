//go:build ignore

package skus

import "context"

// A hot read forks a second residual-candidate source instead of the one helper.
func leftoverCandidates(ctx context.Context, residual []Sku) []Candidate {
	return ratecardmatch.LeftoverTopK(ctx, residual) // want: arbitrator-residual-single-source
}
