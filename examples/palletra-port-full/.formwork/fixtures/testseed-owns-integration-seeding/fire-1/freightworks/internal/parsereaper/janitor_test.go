//go:build ignore

package parsereaper

func TestJanitorPass(t *testing.T) {
	_, _ = pool.Exec(ctx, "INSERT INTO palletra.projects (id, org_id) VALUES ($1, $2)", pid, oid) // want: testseed-owns-integration-seeding
}
