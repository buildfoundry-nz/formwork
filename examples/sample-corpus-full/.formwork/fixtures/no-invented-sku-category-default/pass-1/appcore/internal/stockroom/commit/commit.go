//go:build ignore

package commit

// commitCatalogEntry writes a merchant-import row into platform.sku_catalog.
// An unclassified row is NULL: pass the caller's value through, and the INSERT
// maps an empty string to SQL NULL (#7329). The honest shapes below stay legal.
func commitCatalogEntry(req catalogRequest) error {
	category := strings.TrimSpace(req.GetCategory())
	if category == "" {
		category = ""
	}
	return insertInventoryRow(req.id, category)
}
