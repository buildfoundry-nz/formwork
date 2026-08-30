//go:build ignore

package spanwire

import "context"

func primeInline(ctx context.Context, pool Pool) {
	pool.Exec(ctx, `INSERT INTO palletra.projects (id) VALUES ($1)`) // want: spanwire-tests-seed-through-helper
}
