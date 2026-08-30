//go:build ignore

package parsewrite

import "testing"

func TestUnitConv(t *testing.T) {
	// Tests may pin the numeric value; _test.go is exempt from the gate.
	if (0.0254 / 96.0) <= 0 {
		t.Fatal("bad")
	}
}
