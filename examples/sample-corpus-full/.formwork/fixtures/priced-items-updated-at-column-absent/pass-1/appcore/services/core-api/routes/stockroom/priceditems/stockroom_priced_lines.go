//go:build ignore

package priceditems

func reassign(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE platform.priced_lines SET vendor_id = $1 WHERE id = $2`)
	return err
}
