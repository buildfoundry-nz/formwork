//go:build ignore

package main

func newPool(ctx context.Context, dsn string) {
	pool, _ := db.NewPool(ctx, dsn)
	_ = pool
}
