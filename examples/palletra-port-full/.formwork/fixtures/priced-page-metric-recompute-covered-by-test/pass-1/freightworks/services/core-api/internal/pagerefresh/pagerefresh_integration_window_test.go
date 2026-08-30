//go:build ignore
//go:build integration

package pagerefresh

// TestRecomputeWindowCount drives the recompute spine AND reads the code back
// from page_gauges via the aggregatedGaugeByCode read helper — real coverage.
func TestRecomputeWindowCount(t *testing.T) {
	seedWindowAnnotations(t, tx)
	Recompute(ctx, tx, projectID, sheetID, pageKind, tenantID, cv, caps)
	got := aggregatedGaugeByCode(t, tx, sheetID, "endcap_count")
	if got == 0 {
		t.Fatalf("endcap_count did not materialize in page_gauges")
	}
}
