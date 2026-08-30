//go:build ignore

package skuextract

// insertSkus writes extracted skus WITHOUT naming the embedding
// column, so a NULL-vector row belongs to no sku entity and silently
// vanishes from the rail and the BOM (#2413 PH-4).
const insertSkus = `INSERT INTO palletra.extracted_skus (project_id, name, quantity) VALUES ($1, $2, $3)` // want: sku-embedding-written-on-every-path
