//go:build ignore

package subcategory

import "testing"

func TestAssignSubcategoryConcurrentDeleteReturns404(t *testing.T) { // want: concurrent-tests-use-sync-primitives
	setup(t)
	got := handleAssign(t)
	if got != 404 {
		t.Fatalf("want 404, got %d", got)
	}
}
