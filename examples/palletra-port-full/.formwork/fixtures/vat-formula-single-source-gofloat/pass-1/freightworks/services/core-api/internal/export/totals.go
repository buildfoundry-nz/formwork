//go:build ignore

package export

import "github.com/palletra/freightworks/internal/vatmath"

func vatAmount(netAmount, vatPercent float64) float64 {
	return vatmath.VatAmount(netAmount, vatPercent)
}
