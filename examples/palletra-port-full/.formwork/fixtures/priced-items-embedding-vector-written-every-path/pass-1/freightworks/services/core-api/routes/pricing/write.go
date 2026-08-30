//go:build ignore

package pricing

// insertRateLine names both canonical_label and embedding and binds the
// fail-closed vector, so every priced_lines row stays matchable by the read
// matcher's exact and similarity tiers.
const insertRateLine = `INSERT INTO platform.priced_lines (org_id, description, canonical_label, embedding, rate) VALUES ($1, $2, $3, $4, $5)`
