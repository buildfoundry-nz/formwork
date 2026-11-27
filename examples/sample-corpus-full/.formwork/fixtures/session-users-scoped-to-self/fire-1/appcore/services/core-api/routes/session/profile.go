//go:build ignore

package session

// loadUser runs under db.AsSuperuser (BYPASSRLS); the users read is scoped only
// by org_id, so it can read any user's row.
func loadUser(tx pgx.Tx, ctx context.Context) error {
	_, err := tx.Exec(ctx, `SELECT email FROM users WHERE org_id = $1`) // want: session-users-scoped-to-self
	return err
}
