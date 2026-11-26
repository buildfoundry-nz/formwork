//go:build ignore

package suppliers

const matchQuery = `
	SELECT id FROM platform.priced_lines pi
	WHERE ps.vendor_id = $1
`
