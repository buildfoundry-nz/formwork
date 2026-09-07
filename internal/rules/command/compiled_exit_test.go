package command_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules/command"
)

// buildExitBinary compiles a tiny main that os.Exit(code). Used to pin
// WithCompiledBinary's go-run exit-collapse parity without depending on PATH shims.
func buildExitBinary(t *testing.T, code int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/exbin\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "package main\nimport \"os\"\nfunc main() { os.Exit(" + strconv.Itoa(code) + ") }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "exbin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// TestWithCompiledBinaryCollapsesNonZeroLikeGoRun pins Finding 1 of formwork
// PR #14: `go run` turns any non-zero program exit into process exit 1 and
// prints "exit status N" on stderr; a raw prebuilt binary keeps the real
// code. Compile-once must reproduce go run, or formwork test disagrees with
// formwork check for any expect.exit other than 0/1.
func TestWithCompiledBinaryCollapsesNonZeroLikeGoRun(t *testing.T) {
	bin := buildExitBinary(t, 2)

	// expect.exit 2 would PASS on a raw binary exit 2, but FAIL under go run
	// (observed 1). Compile-once must take the go-run side: fire with exited 1
	// and surface the real code in an "exit status 2" line.
	base := build(t, "cmd: [go, run, .]\nexpect: {exit: 2}")
	c, ok := command.WithCompiledBinary(base, bin, nil, "")
	if !ok {
		t.Fatal("WithCompiledBinary must accept a command checker")
	}
	m, err := finalize(t, c)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("want one finding (exit collapsed to 1 ≠ expect 2), got %v", m)
	}
	msg := m[0].Message
	if !strings.Contains(msg, "exited 1") {
		t.Fatalf("observed exit must be 1 (go run parity), not raw 2; got %q", msg)
	}
	if strings.Contains(msg, "exited 2") {
		t.Fatalf("must not report raw binary exit 2; got %q", msg)
	}
	if !strings.Contains(msg, "exit status 2") {
		t.Fatalf("combined output must carry go-run's exit status line; got %q", msg)
	}
}

func TestWithCompiledBinaryExitOneMatchesGoRunExpect(t *testing.T) {
	// Detector exits 2; go run → process exit 1. expect.exit 1 must pass
	// under compile-once the same way cold go run would.
	bin := buildExitBinary(t, 2)
	base := build(t, "cmd: [go, run, .]\nexpect: {exit: 1}")
	c, ok := command.WithCompiledBinary(base, bin, nil, "")
	if !ok {
		t.Fatal("WithCompiledBinary must accept a command checker")
	}
	m, err := finalize(t, c)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("exit 2 under compile-once must observe as 1 and pass expect.exit 1; got %v", m)
	}
}

func TestWithCompiledBinaryZeroExitUnchanged(t *testing.T) {
	bin := buildExitBinary(t, 0)
	base := build(t, "cmd: [go, run, .]")
	c, ok := command.WithCompiledBinary(base, bin, nil, "")
	if !ok {
		t.Fatal("WithCompiledBinary must accept a command checker")
	}
	m, err := finalize(t, c)
	if err != nil || len(m) != 0 {
		t.Fatalf("exit 0 must stay a pass with no exit-status line forced; got matches=%v err=%v", m, err)
	}
}
