//go:build ignore

package orgsetup_test

// primeInvitedMembership hand-rolls the invited-membership INSERT instead of
// routing through the testsupport seeder — the drift class that shipped the
// non-existent 'VIEWER' native_role (sweep-4 #9).
func primeInvitedMembership() string {
	return "INSERT INTO memberships (user_id, org_id, native_role, status) VALUES ($1, $2, 'estimator', 'invited')" // want: testsupport-seeds-invited-memberships
}
