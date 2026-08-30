//go:build ignore

package bomtotals

import "github.com/palletra/freightworks/internal/vatmath"

func grossSQL() string {
	return vatmath.VatIncSQL("ex", "vat_percent")
}
