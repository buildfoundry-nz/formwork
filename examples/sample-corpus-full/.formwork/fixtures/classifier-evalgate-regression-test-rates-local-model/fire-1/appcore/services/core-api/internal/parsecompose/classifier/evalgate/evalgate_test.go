//go:build ignore

package evalgate

import "testing"

// Regression test exists but never exercises RateLocalModel — the gate is
// vacuous, the anchor is missing.
func TestPlaceholder(t *testing.T) {
	_ = "no scoring exercised"
}
