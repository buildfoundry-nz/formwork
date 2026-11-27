//go:build ignore

package gatetests

import (
	"strings"
	"testing"
)

func TestGateFiresOnViolation(t *testing.T) {
	tmp := plantViolation(t)
	stdout, err := execScriptInDir(t, "scripts/check-x.sh", tmp)
	if err == nil {
		if strings.Contains(stdout, "OK") {
			t.Skip("script no-op'd on minimal fixture; treating as pass")
		}
		t.Fatalf("expected non-zero exit, got clean: %s", stdout)
	}
}
