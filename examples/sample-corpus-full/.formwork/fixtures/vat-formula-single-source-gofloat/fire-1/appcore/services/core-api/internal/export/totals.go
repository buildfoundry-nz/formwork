//go:build ignore

package export

func vatAmount(netAmount, vatPercent float64) float64 {
	return netAmount * vatPercent / 100 // want: vat-formula-single-source-gofloat
}
