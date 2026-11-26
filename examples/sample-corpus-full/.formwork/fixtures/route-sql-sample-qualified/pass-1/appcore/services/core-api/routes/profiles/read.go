//go:build ignore

package profiles

// readProfile schema-qualifies the sample table, the universal convention.
func readProfile() string {
	return "SELECT id, name FROM sample.account_profiles WHERE id = $1"
}
