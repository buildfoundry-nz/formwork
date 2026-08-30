//go:build ignore

package pricing

// reprice hand-writes the BOM money aggregates instead of calling
// bomtotals.Recompute — the handoff-A defect.
func reprice() string {
	return "UPDATE palletra.boms SET total_ex_vat = $1, total_inc_vat = $2 WHERE id = $3" // want: bom-totals-only-in-recompute
}
