//go:build ignore

package deckinputs

// loadFieldsQuery reads FROM palletra.extracted_fields without a `superseded`
// predicate, so it folds every past generation and a stale high-confidence row
// can beat the corrected current value (sweep-10 PER-2).
const loadFieldsQuery = `SELECT panel_grade FROM palletra.extracted_fields WHERE project_id = $1` // want: extracted-facts-reads-exclude-superseded
