//go:build ignore

package pricing

// insertRateLine names canonical_label and binds the computed canonical
// fold, so every priced_lines row stays matchable by the read-time matcher.
const insertRateLine = `INSERT INTO platform.priced_lines (org_id, description, canonical_label, rate) VALUES ($1, $2, $3, $4)`
