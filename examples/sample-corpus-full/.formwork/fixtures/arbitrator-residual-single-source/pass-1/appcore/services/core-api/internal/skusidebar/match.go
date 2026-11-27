//go:build ignore

package skusidebar

import "context"

// The one sanctioned residual-candidate helper (exempt location).
func composeResidualCandidates(ctx context.Context, residual []Sku) []Candidate {
	return ratecardmatch.LeftoverTopK(ctx, residual)
}
