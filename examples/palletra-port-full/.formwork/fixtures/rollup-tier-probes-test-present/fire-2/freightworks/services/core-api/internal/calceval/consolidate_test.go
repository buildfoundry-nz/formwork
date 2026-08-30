//go:build ignore

package calceval

import "testing"

// FIRE: the end-to-end tier probes have been deleted.
func TestDedupSimple(t *testing.T) {
	_ = t
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func TestDedup_EndToEnd_TierProbes(t *testing.T) {
