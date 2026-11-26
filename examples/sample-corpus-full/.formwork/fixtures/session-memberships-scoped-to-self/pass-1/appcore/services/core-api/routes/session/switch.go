//go:build ignore

package session

// swapOrg carries the claims-bound self-scope predicate, so the BYPASSRLS
// write can only touch the caller's own membership row.
func swapOrg(tx pgx.Tx, ctx context.Context) error {
	_, err := tx.Exec(ctx, `UPDATE memberships SET is_active = false WHERE user_id = $1`)
	return err
}
