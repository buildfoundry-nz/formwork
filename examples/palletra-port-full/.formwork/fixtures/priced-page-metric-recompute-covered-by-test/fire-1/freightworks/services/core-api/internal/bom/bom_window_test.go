//go:build ignore
//go:build integration

package bom

// TestWindowBOMLine names endcap_count in passing while asserting a painting BOM
// line — it neither drives Recompute nor reads palletra.page_gauges, so it is
// NOT in the recompute-coverage corpus (closeout #12: a bare mention is not
// coverage).
func TestWindowBOMLine(t *testing.T) {
	line := bomLineFor("endcap_count")
	if line.Qty != 4 {
		t.Fatalf("bad qty")
	}
}
