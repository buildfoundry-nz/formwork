//go:build ignore

package spanwire

import "context"

// prime_integration_test.go is the one sanctioned home for the raw seed STOREs,
// so it is excluded from the gate — a raw INSERT here must NOT fire.
func primeDimPage(t T, ctx context.Context, pool Pool, slug string) {
	pool.Exec(ctx, `INSERT INTO platform.organizations (slug) VALUES ($1)`, slug)
	pool.Exec(ctx, `INSERT INTO palletra.projects (org_id) VALUES ($1)`)
}
