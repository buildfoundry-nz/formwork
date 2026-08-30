//go:build ignore

package runlines

// insertCrossbeamLine re-types the managed bom_line_items INSERT inline instead of
// calling the shared writer — the exact drift #1920 forbids.
const insertCrossbeamLineQuery = `INSERT INTO palletra.bom_line_items (project_id, description, unit, quantity, rate, source, ordinal) SELECT b.project_id, $2, $3, $4, $5, 'auto_derived'::palletra.line_origin, $8 FROM palletra.boms b WHERE b.id = $1` // want: single-source-managed-line-insert
