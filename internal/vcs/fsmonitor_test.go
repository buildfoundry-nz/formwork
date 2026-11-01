package vcs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// git consults core.fsmonitor on every index read, and waits for the answer.
// A wedged fsmonitor daemon therefore stalls `git ls-files` forever, and a
// pre-commit's check waited on one for ten minutes while holding a machine-wide
// hook lock, which stopped every commit on the machine. fsmonitor only speeds
// up how git finds changed paths, never what it answers, so the engine's git
// calls read the index without it.
//
// The hook here stands in for the wedged daemon: it answers nothing for longer
// than the whole test allows.
func TestGitCallsDoNotWaitOnAnFSMonitor(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, ".gitignore", "build/\n")
	write(t, dir, "build/out.bin", "x")
	run(t, dir, "add", "a.go", ".gitignore")

	hook := filepath.Join(t.TempDir(), "stalled-fsmonitor")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nsleep 8\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "config", "core.fsmonitor", hook)

	start := time.Now()
	staged, err := vcs.StagedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	ignored, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 6*time.Second {
		t.Fatalf("the engine's git calls waited %s on core.fsmonitor; they must not consult it", took.Round(time.Millisecond))
	}
	if len(staged) != 2 {
		t.Errorf("staged = %v, want a.go and .gitignore: dropping fsmonitor must not change the answer", staged)
	}
	if len(ignored) != 1 {
		t.Errorf("ignored = %+v, want build/ alone: dropping fsmonitor must not change the answer", ignored)
	}
}
