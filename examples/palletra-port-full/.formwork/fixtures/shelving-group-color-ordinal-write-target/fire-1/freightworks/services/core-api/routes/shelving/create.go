//go:build ignore

package shelving

// createBatch writes the server-minted color + ordinal straight from the
// handler — the #6724 defect.
func createBatch() string {
	return "INSERT INTO palletra.shelving_groups (project_id, shelving_type_code, color, ordinal) VALUES ($1, $2, $3, $4)" // want: shelving-group-color-ordinal-write-target
}
