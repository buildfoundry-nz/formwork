package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// progressRepo is a corpus whose single rule is a FINALIZER (required-pattern
// in exists mode evaluates once per run, not per file), because -progress
// reports finalizer completion — a per-file rule has no single completion
// moment to report. The pattern is present, so the rule PASSES and the run
// exits 0: progress must be able to stream on a green run, not only on red
// ones (a liveness signal that only appears when things are already wrong
// would be exactly the silent-success shape it exists to break).
func progressRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: has-widget\n    type: required-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET, mode: exists}\n")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const x = \"WIDGET\"\n")
	return root
}

// -progress. A whole-corpus check writes its report only at the end, so a
// long run is silent for minutes and an operator cannot tell a slow rule from
// a hung one. -progress streams one stderr line per completed finalizer
// (rule id + cumulative ms). Three contracts are pinned: the lines reach
// stderr on a GREEN run; the verdict on stdout is untouched; and a run
// WITHOUT the flag writes no progress lines at all — progress is opt-in,
// never a change to the default output contract.
func TestCheckProgressStreamsFinalizerLinesToStderr(t *testing.T) {
	root := progressRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root, "-progress")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (the rule passes)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[has-widget] OK") {
		t.Fatalf("verdict must reach stdout:\n%s", out)
	}
	if !strings.Contains(errOut, "formwork: progress rule=has-widget") {
		t.Fatalf("a progress line per finalizer must reach stderr:\n%s", errOut)
	}
	if strings.Contains(out, "formwork: progress") {
		t.Fatalf("progress must never reach stdout:\n%s", out)
	}
}

func TestCheckWithoutProgressIsSilentOnStderr(t *testing.T) {
	root := progressRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "formwork: progress") {
		t.Fatalf("default runs must not stream progress lines:\n%s", errOut)
	}
}
