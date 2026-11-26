//go:build ignore

package suppliers

const impactQuery = `
	SELECT count(*) FROM platform.priced_lines pi
	WHERE ps.vendor_id = $1
`
