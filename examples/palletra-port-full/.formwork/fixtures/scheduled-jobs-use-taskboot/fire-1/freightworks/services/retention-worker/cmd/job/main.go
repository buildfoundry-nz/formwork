//go:build ignore

package main

func main() {
	// Hand-rolls its own pool bootstrap instead of routing through
	// taskboot.OpenDBPool — exactly the drift #1920 folded away.
	pool, err := dbConnect(ctx, dsn)
	if err != nil {
		panic(err)
	}
	run(pool)
}
