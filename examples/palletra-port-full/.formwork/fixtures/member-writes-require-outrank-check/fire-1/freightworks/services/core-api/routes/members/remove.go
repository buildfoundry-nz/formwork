//go:build ignore

package members

// Remove deletes a membership WITHOUT loading the target's current tier, so a
// delegated-USERS_MANAGE non-admin can remove a higher-tier member (sweep-4 #1).
func Remove(crewID string, claims *Claims, tx Tx) error {
	_, err := tx.Exec(`DELETE FROM memberships WHERE id = $1`, crewID) // want: member-writes-require-outrank-check
	return err
}
