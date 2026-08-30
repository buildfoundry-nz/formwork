//go:build ignore

package geometrymut

func record(ctx context.Context, tx pgx.Tx, id string) {
	row, _ := mutbase.LockAndFetchContext(ctx, tx, id) // want: single-row-mutators-editable-gate
	_ = row
}
