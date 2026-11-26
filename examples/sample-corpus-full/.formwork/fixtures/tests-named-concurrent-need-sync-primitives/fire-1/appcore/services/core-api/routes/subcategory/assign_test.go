//go:build ignore

package subcategory

import "testing"

func TestAssignTierConcurrentDeleteReturns404(t *testing.T) { // want: tests-named-concurrent-need-sync-primitives
	setup(t)
	got := handleAllocate(t)
	if got != 404 {
		t.Fatalf("want 404, got %d", got)
	}
}
