//go:build ignore

package stockroom

const SupplierQueryColumns = `
	count(*) FILTER (WHERE ps.vendor_id = s.id) AS item_count
`
