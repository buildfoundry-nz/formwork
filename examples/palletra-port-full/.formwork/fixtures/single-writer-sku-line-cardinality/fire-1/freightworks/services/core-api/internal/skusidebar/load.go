//go:build ignore

package skusidebar

// ManualLine is one authored BOM line for a sku.
type ManualLine struct{ ID string }

// loadManualLines keys each sku to ONE line and drops the rest — the
// #4623 last-write-wins overwrite over undefined Postgres order.
func loadManualLines() {
	bySku := map[string]ManualLine{} // want: single-writer-sku-line-cardinality
	_ = bySku
}
