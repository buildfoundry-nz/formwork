//go:build ignore

package main

func newPool(ctx context.Context, dsn string) {
	pool, _ := pgxpool.New(ctx, dsn) // want: sole-pool-constructor
	_ = pool
}
