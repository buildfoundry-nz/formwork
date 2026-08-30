//go:build ignore

package shelving

// A create that sends only the type code — color and ordinal are server-minted
// in internal/shelvinggroups and absent from this write.
func createBatch() string {
	return "INSERT INTO palletra.shelving_groups (project_id, shelving_type_code) VALUES ($1, $2)"
}
