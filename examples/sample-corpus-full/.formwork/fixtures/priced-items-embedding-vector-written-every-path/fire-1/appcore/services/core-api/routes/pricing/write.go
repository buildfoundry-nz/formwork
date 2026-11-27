//go:build ignore

package pricing

// insertRateLine folds the canonical descriptor but never names the embedding
// column, so the read matcher's similarity tier can never cosine this row and the
// product is silently mis-priceable.
const insertRateLine = `INSERT INTO platform.priced_lines (org_id, description, canonical_label, rate) VALUES ($1, $2, $3, $4)` // want: priced-items-embedding-vector-written-every-path
