//go:build ignore

package subcategory

import "testing"

// The retired opt-out comment no longer satisfies the rule: a *Concurrent*
// test must coordinate real concurrency.
func TestAssignSubcategoryConcurrentDeleteReturns404(t *testing.T) { // want: concurrent-tests-use-sync-primitives
	// audit-16-#5-concurrent-allowlist: sequential is fine here
	setup(t)
	got := handleAssign(t)
	if got != 404 {
		t.Fatalf("want 404, got %d", got)
	}
}
