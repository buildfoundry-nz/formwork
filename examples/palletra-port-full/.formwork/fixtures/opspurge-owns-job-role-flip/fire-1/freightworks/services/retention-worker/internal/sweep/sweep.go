//go:build ignore

package sweep

import "context"

// A hand-opened tx with a raw role flip — must route through internal/opspurge instead.
func toggleRole(ctx context.Context, tx DbTx) error {
	_, err := tx.Exec(ctx, "SET LOCAL ROLE runtime_root_admin") // want: opspurge-owns-job-role-flip
	return err
}
