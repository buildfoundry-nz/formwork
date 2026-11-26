//go:build ignore

package gatetests

import "testing"

func TestGateFiresOnViolation(t *testing.T) {
	tmp := plantViolation(t)
	_, err := execScriptInDir(t, "scripts/check-x.sh", tmp)
	if err == nil {
		t.Fatalf("expected the gate to fire on the planted violation")
	}
}
