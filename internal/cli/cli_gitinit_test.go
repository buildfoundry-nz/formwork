package cli_test

// Split from cli_test.go when the 750-line vendor cap fired (same precedent as
// rulesfor_gitignore_test.go). Guards the gitInit helper itself; the helpers
// under guard live in cli_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitInitSpawnsNoDetachedMaintenance pins the fix for a CI flake (main run
// 32901235204, darwin): testing.TempDir's RemoveAll once failed with
// "unlinkat .../001/.git: directory not empty" in this package's cleanup phase.
// The writer racing the removal is git itself: `git commit` forks a DETACHED
// `git maintenance run --auto --quiet --detach` child that outlives the commit
// process, and that child creates `.git/objects/maintenance.lock` while it
// evaluates — and on a tiny repo, rejects — every auto-maintenance condition.
// Both halves were observed directly on git 2.50.1: the spawn via GIT_TRACE,
// and the lock file via a directory poller (105 sightings across 300 runs).
// gitInit must leave the repo with that child disarmed, or every test here
// that commits re-opens the same race.
//
// GIT_TRACE is a file path, so it is inherited by the detached grandchild as
// well — but the line this test reads is written by the COMMIT process itself
// ("run_command: git maintenance run --auto ...") before it exits, which
// gitRun waits for. The assertion is therefore not timing-dependent.
func TestGitInitSpawnsNoDetachedMaintenance(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "trace.log")
	t.Setenv("GIT_TRACE", trace)
	root := t.TempDir()
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, "a.txt"), "x\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-qm", "init")

	content, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("git wrote no trace file: %v", err)
	}
	if strings.Contains(string(content), "maintenance run") {
		t.Errorf("git commit spawned a detached maintenance child that can outlive the test:\n%s", content)
	}
}
