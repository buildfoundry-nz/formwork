//go:build ignore

package spanwire

import "context"

func primeViaHelper(t T, ctx context.Context, pool Pool, slug string) {
	primeDimPage(t, ctx, pool, slug)
	pool.Query(ctx, `SELECT id FROM palletra.pages WHERE project_id = $1`)
}
