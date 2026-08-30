//go:build ignore

package skuextract

// insertSkus names the embedding column and binds a real vector for every
// sku row, so the grouping pass can attach each sighting (#2413 PH-4).
const insertSkus = `INSERT INTO palletra.extracted_skus (project_id, name, quantity, embedding) VALUES ($1, $2, $3, $4)`
