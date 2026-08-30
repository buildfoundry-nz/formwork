//go:build ignore

package skus

func promote(ctx context.Context, mat Sku) {
	// ratecardmatch.DualMatch is a per-pair primitive and stays allowed.
	m, _ := skusidebar.ComputeEffectiveMatch(ctx, mat)
	_ = m
}
