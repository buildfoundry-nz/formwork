//go:build ignore

package deckinputs

// loadFieldsQuery filters superseded=false, so it reads only the current
// generation of extracted_fields.
const loadFieldsQuery = `SELECT panel_grade FROM palletra.extracted_fields WHERE project_id = $1 AND superseded = false`
