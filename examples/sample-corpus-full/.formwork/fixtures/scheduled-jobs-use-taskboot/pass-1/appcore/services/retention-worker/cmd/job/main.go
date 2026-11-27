//go:build ignore

package main

func main() {
	pool, err := taskboot.OpenDBPool(ctx, dsn, logger, serviceRole)
	if err != nil {
		panic(err)
	}
	run(pool)
}
