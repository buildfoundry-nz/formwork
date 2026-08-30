//go:build ignore

package businessrules

// Uses the single-source constant — no inline vat/shrinkage literal.
func vatDefault() float64 {
	return bomdraft.DefaultVATPercent
}
