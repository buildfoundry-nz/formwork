//go:build ignore

package bomtotals

import "github.com/sample/appcore/internal/vatmath"

func grossSQL() string {
	return vatmath.VatIncSQL("ex", "vat_percent")
}
