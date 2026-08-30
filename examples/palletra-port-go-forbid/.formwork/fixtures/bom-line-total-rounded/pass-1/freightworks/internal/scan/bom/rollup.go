//go:build ignore

package bom

// pricedTotal builds the priced rollup query using the rounded expression.
func pricedTotal() string {
	return `SELECT SUM(` + SnappedLineTotalExpr + `) FROM bom_line_items bli`
}
