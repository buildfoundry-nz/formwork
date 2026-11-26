//go:build ignore

package pricing

// insertRateLine writes a priced_lines row WITHOUT naming the canonical column,
// so the row can never match an extracted sku and is silently mis-priceable.
const insertRateLine = `INSERT INTO platform.priced_lines (org_id, description, rate) VALUES ($1, $2, $3)` // want: priced-items-canonical-descriptor-written-every-path
