//go:build ignore
//go:build gate

package gatetests

import "testing"

// FIG-LEAF: this synth NAMES scripts/check-example-gate.go but only runs it against a
// CLEAN fixture and asserts it passed — it never drives the gate to fire, so a
// regression the gate should catch would sail through.
func TestCheckFooSynthetic(t *testing.T) {
	tmp := t.TempDir()
	_, err := execScriptInDir(t, "scripts/check-example-gate.go", tmp)
	if err != nil {
		t.Fatalf("check-example-gate.go should pass on a clean fixture: %v", err)
	}
}
