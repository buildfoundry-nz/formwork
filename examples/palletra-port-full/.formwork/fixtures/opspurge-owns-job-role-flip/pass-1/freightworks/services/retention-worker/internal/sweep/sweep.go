//go:build ignore

package sweep

import "context"

// The sanctioned flip: role change goes through internal/opspurge.AsAdminTx.
func toggleRole(ctx context.Context, pool Pool) error {
	return opspurge.AsAdminTx(ctx, pool, func(tx DbTx) error {
		return nil
	})
}
