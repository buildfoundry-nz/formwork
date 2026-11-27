//go:build ignore

package auditwriter

// Every INSERTed sample.* table below is also READ via a FROM/JOIN, so no
// table is a write-only dead-end.
const insertTimeline = `INSERT INTO sample.annotation_timeline (annotation_id, snapshot) VALUES ($1, $2)`

const readTimeline = `SELECT snapshot FROM sample.annotation_timeline WHERE annotation_id = $1`

const insertSpan = `INSERT INTO sample.measure_label_association_segments (chain_id, seg) VALUES ($1, $2)`

const readSpan = `SELECT seg FROM sample.measure_label_association_segments WHERE chain_id = $1`
