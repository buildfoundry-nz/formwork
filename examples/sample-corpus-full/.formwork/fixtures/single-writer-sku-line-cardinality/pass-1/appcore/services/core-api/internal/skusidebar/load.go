//go:build ignore

package skusidebar

// ManualLine is one authored BOM line for a sku.
type ManualLine struct{ ID string }

// loadManualLines fans out one entry per authored line, so a KEEP_BOTH pair
// both survive to the sidebar. A sku-keyed map[string]ManualLine here
// would drop the second line — banned.
func loadManualLines() {
	bySku := map[string][]ManualLine{}
	_ = bySku
}
