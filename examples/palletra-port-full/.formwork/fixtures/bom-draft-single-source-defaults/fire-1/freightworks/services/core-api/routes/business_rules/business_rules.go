//go:build ignore

package businessrules

// Inlines the VAT default literal instead of using bomdraft.DefaultVATPercent.
func vatDefault() float64 {
	vatPercent := 12.5 // want: bom-draft-single-source-defaults
	return vatPercent
}
