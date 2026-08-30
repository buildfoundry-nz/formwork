//go:build ignore

package projectguard

// The seam holds the ONLY copy of the policy-read SQL — country and
// facility_type together with palletra.projects — and scope excludes this
// directory, which is what makes it structurally the only allowed home.
const policyRead = `SELECT country, facility_type::text FROM palletra.projects WHERE id = $1`

// Read runs the policy read under the caller's typed lock mode.
func Read(ctx Context, tx Tx, projectID string, mode LockChoice) (Policy, error) {
	return readPolicyRow(tx.QueryRow(ctx, policyRead+mode.Suffix(), projectID))
}
