//go:build ignore

package members

// Remove rejects a target that out-ranks the caller before deleting, in the
// same tx, via the shared target-tier guard (sweep-4 #1).
func Remove(ctx Ctx, crewID string, claims *Claims, tx Tx) error {
	if err := shared.AssertCallerSeniorToTarget(ctx, tx, crewID, claims.GetAccessGrade()); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM memberships WHERE id = $1`, crewID)
	return err
}
