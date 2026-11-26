//go:build ignore

package session

// swapOrg runs under db.AsSuperuser (BYPASSRLS); the memberships write is
// scoped only by org_id, so any caller could re-point another user's row.
func swapOrg(tx pgx.Tx, ctx context.Context) error {
	_, err := tx.Exec(ctx, `UPDATE memberships SET is_active = false WHERE org_id = $1`) // want: session-memberships-scoped-to-self
	return err
}
