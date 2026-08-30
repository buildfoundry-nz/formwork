//go:build ignore

package routes

import "context"

func relabelAnnotation(ctx context.Context, tx dbTx, id, label string) error {
	_, err := tx.Exec(ctx, `UPDATE palletra.annotations SET label=$1 WHERE id=$2`, label, id) // want: no-unrouted-annotation-update
	return err
}
