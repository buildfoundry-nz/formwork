//go:build ignore

package bom

// pricedTotal builds the priced rollup query.
func pricedTotal() string {
	return `SELECT SUM(bli.total) FROM bom_line_items bli` // want: bom-line-total-rounded
}
