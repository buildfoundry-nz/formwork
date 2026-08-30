//go:build ignore
//go:build gate

package gatetests

import "testing"

// FIRE-ON-VIOLATION: this synth runs scripts/check-example-gate.go against a KNOWN-BAD
// fixture and demands a NON-zero exit — proving the gate actually fires.
func TestCheckFooFiresOnViolation(t *testing.T) {
	tmp := writeBadFixture(t)
	_, err := execScriptInDir(t, "scripts/check-example-gate.go", tmp)
	if err == nil {
		t.Fatalf("check-example-gate.go must FAIL on the known-bad fixture, but it passed")
	}
}
