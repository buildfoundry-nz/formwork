//go:build ignore

package main

func openDBPool(ctx context.Context, dsn string) *pgxpool.Pool {
	pool, err := taskboot.OpenDBPool(ctx, dsn, logger, serviceRole)
	if err != nil {
		panic(err)
	}
	return pool
}
