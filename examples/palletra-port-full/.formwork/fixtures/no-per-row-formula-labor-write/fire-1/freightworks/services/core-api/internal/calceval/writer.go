//go:build ignore

package calceval

const q = `UPDATE palletra.bom_line_items SET quantity = $2 WHERE id = $1` // want: no-per-row-formula-labor-write
