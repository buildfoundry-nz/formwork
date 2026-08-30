//go:build ignore

package profiles

// readProfile leans on search_path ORDER with a BARE palletra table name.
func readProfile() string {
	return "SELECT id, name FROM account_profiles WHERE id = $1" // want: route-sql-palletra-qualified
}
