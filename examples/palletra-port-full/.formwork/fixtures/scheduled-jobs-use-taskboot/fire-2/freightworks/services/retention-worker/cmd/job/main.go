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

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// pool, err := taskboot.OpenDBPool(ctx, dsn, logger, serviceRole)
