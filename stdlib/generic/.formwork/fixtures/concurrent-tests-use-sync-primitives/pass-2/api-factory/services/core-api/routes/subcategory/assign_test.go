//go:build ignore

package subcategory

import "testing"

// The interleave lives in ONE shared helper, so the goroutine and channel are
// not in this file. That is the point: a second copy of a delicate interleave
// is where the ordering silently stops being an interleave (#9874). The helper
// is what proves the concurrency, exactly as the pre-existing `pgxtest.`
// alternative does.
func TestAssignSubcategoryConcurrentDeleteReturns404(t *testing.T) {
	done, err := StartRivalCapture(t)
	if err != nil {
		t.Fatalf("start rival: %v", err)
	}
	got := handleAssign(t)
	if rerr := <-done; rerr != nil {
		t.Fatalf("rival interleave: %v", rerr)
	}
	if got != 404 {
		t.Fatalf("want 404, got %d", got)
	}
}
