//go:build ignore

package profiles

// readProfile schema-qualifies the palletra table, the universal convention.
func readProfile() string {
	return "SELECT id, name FROM palletra.account_profiles WHERE id = $1"
}
