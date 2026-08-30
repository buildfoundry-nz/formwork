//go:build ignore

package calceval

const q = `UPDATE palletra.bom_line_items AS b SET quantity = u.q FROM unnest($1::uuid[], $2::numeric[]) AS u(id, q) WHERE b.id = u.id`
