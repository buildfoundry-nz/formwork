package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// --workers validation (#156). A supplied flag that is silently discarded is
// the same shape as the `--range ""` fallback PR #154 refused one screen up in
// cli.go: the operator typed a value, the run did the opposite, and nothing on
// stderr pointed at the flag. Here the discarded value is a concurrency
// throttle, so the symptom is a machine at full width rather than a wrong file
// set.
//
// Two commands accept the flag — `check` and `test` — and both are pinned. The
// `--range` guard had to be applied to runCheck AND runScope, and missing the
// second caller was itself a review finding; the same two-site shape is here.

// workersRepo is a corpus whose single rule fires, so a run that reaches the
// engine is distinguishable (exit 1) from one refused at the flag (exit 2).
func workersRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")
	return root
}

func TestCheckNegativeWorkersIsRefused(t *testing.T) {
	root := workersRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root, "--workers", "-4")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (a supplied throttle must not be discarded)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "--workers") {
		t.Errorf("stderr must name the flag that was refused:\n%s", errOut)
	}
	if strings.Contains(out, "rules passed") || strings.Contains(out, "[no-widget]") {
		t.Errorf("the run must be refused before the engine, not reported over it:\n%s", out)
	}
}

// The other half of the guard, and the more important one: `--workers 0` is
// BOTH the flag's default and a legal supplied value, and both mean GOMAXPROCS.
// A guard that refused the default would be a worse bug than the one being
// fixed — every invocation of check would exit 2.
func TestCheckZeroWorkersStillRuns(t *testing.T) {
	root := workersRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root, "--workers", "0")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (0 means GOMAXPROCS, exactly as an absent flag does)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[no-widget] FAIL") {
		t.Fatalf("the run must have reached the engine:\n%s", out)
	}
}

// `test` is the second command that accepts --workers, and it hands the value
// to fixturetest.Run, which passes it straight to the same engine default. The
// flag is refused there too, and before the config-CONTENT guards: a caller who
// mistyped a flag needs to hear about the flag.
func TestTestNegativeWorkersIsRefused(t *testing.T) {
	code, out, errOut := runCLI(t, "test", "-C", filepath.Join("testdata", "toyrepo"), "--workers", "-1")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "--workers") {
		t.Errorf("stderr must name the flag that was refused:\n%s", errOut)
	}
	if strings.Contains(out, "fixture(s) run") {
		t.Errorf("fixtures ran under a refused flag:\n%s", out)
	}
}

func TestTestZeroWorkersStillRuns(t *testing.T) {
	code, out, errOut := runCLI(t, "test", "-C", filepath.Join("testdata", "toyrepo"), "--workers", "0")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "fixture(s) run") {
		t.Fatalf("the fixtures must have run:\n%s", out)
	}
}
