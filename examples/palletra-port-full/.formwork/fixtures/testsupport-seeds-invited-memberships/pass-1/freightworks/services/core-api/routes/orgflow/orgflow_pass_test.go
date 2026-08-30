//go:build ignore

package orgsetup_test

// primeViaHelper routes the invited-membership seed through the shared
// testsupport seeder — no hand-rolled INSERT, so the native_role is validated
// against the platform enum in one place.
func primeViaHelper() {
	testsupport.PrimeInvitedMembershipFor(tenantID, userID)
}
