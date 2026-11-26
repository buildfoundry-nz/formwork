//go:build ignore

package bomtotals

const grossSQL = `ROUND((ex) * (1 + vat_percent::numeric / 100), 2)` // want: vat-formula-single-source-sql
