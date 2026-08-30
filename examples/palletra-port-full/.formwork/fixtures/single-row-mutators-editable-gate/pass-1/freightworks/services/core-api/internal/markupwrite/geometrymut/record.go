//go:build ignore

package geometrymut

func record(ctx context.Context, tx pgx.Tx, id string) {
	row, _ := mutbase.LockWritableContext(ctx, tx, id)
	_ = row
}
