//go:build ignore

package commit

// commitCatalogEntry writes a merchant-import row into platform.sku_catalog.
// A price-list row like `FERROSTOCK SHELF CLIPS 40mm B12` has no building-sku
// class, but this write boundary invents one instead of leaving it unclassified.
func commitCatalogEntry(req catalogRequest) error {
	category := "general" // want: no-invented-sku-category-default
	return insertInventoryRow(req.id, category)
}
