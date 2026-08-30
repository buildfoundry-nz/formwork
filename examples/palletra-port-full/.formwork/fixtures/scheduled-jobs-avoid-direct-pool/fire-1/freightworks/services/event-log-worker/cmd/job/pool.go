//go:build ignore

package main

func openDBPool(ctx context.Context, dsn string) *pgxpool.Pool {
	pool, err := db.NewPool(ctx, dsn) // want: scheduled-jobs-avoid-direct-pool
	if err != nil {
		panic(err)
	}
	return pool
}
