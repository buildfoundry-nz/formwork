//go:build ignore

package bom

// SnappedLineTotalExpr is the canonical priced line-total SQL expression.
const SnappedLineTotalExpr = `/* rounded line total */`

// pricedRollup sums the rounded expression, never the raw generated column.
func pricedRollup() string {
	return `SELECT SUM(` + SnappedLineTotalExpr + `) FROM bom_line_items bli`
}
