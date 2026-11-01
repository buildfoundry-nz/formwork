package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// A forbidden-pattern census shells out, and --skip-escapes exists to drop
// shell-outs. Dropping this one would make the pre-commit hook report a pass
// for a detector that never ran. The rule stays fast, so the flag keeps it,
// and a non-zero detector is still a finding.
func TestCheckSkipEscapesKeepsCensus(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"), ""+
		"rules:\n"+
		"  - id: census-fires\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n"+
		"    params:\n      census:\n        cmd: [sh, -c, 'echo folded-document; exit 1']\n"+
		"  - id: command-fires\n    type: command\n    scope: {include: ['**/*.go']}\n"+
		"    params: {cmd: [sh, -c, 'exit 1']}\n")
	mustWrite(t, filepath.Join(root, "a.go"), "package main\n")

	code, out, _ := runCLI(t, "check", "-C", root, "--skip-escapes")
	if code != 1 {
		t.Fatalf("skip-escapes must still fail a firing census, exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, "census-fires") || !strings.Contains(out, "folded-document") {
		t.Fatalf("the census finding must be in the check output:\n%s", out)
	}
	if strings.Contains(out, "census-fires: did not run") {
		t.Fatalf("skip-escapes dropped the census:\n%s", out)
	}
	if !strings.Contains(out, "command-fires: did not run") {
		t.Fatalf("skip-escapes must still drop a type:command rule:\n%s", out)
	}

	quiet := t.TempDir()
	mustWrite(t, filepath.Join(quiet, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(quiet, ".formwork", "rules", "r.yaml"), ""+
		"rules:\n"+
		"  - id: census-quiet\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n"+
		"    params:\n      census:\n        cmd: [sh, -c, 'exit 0']\n")
	mustWrite(t, filepath.Join(quiet, "a.go"), "package main\n")
	code, out, _ = runCLI(t, "check", "-C", quiet, "--skip-escapes")
	if code != 0 || !strings.Contains(out, "[census-quiet] OK") {
		t.Fatalf("a silent census must pass under skip-escapes, exit=%d\n%s", code, out)
	}
	if strings.Contains(out, "census-quiet: did not run") {
		t.Fatalf("a passing census was dropped:\n%s", out)
	}
}
