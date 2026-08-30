//go:build ignore

package priceditems

func touch(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE platform.priced_lines SET updated_at = now() WHERE id = $1`) // want: priced-items-updated-at-column-absent
	return err
}
