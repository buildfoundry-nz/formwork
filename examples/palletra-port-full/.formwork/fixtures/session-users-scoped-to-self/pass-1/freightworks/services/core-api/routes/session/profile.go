//go:build ignore

package session

// loadUser carries the bare-column self-scope predicate (users.id IS the user
// id), so the BYPASSRLS read can only return the caller's own row.
func loadUser(tx pgx.Tx, ctx context.Context) error {
	_, err := tx.Exec(ctx, `SELECT email FROM users WHERE id = $1`)
	return err
}
