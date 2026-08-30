package hooks_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// THE ONLY TEST IN THIS PACKAGE WHERE GIT'S OWN EXIT STATUS IS THE ASSERTION.
//
// Every other test here — and every test that existed before #146 — writes files
// with Install and reads them with Verify. Those tests do run git (init, config,
// worktree add, and verify_test.go's commitEverything makes a seed commit before
// any shim is wired), but only as setup: the verdict they assert on is
// formwork's own. That loop cannot notice git disagreeing with both, which is
// exactly how five fail-open rows survived — install wrote, verify read, the
// test agreed, and no hook ran. This one commits against an installed shim and
// lets git's exit status be the assertion.
//
// Both directions, because a negative-only proof passes just as well against a
// gate that always fails (this repo's own argument, scripts/gate-proof.sh).

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// buildFormwork builds the real binary once per test run. The shim resolves
// `formwork` on PATH (#146 D4), so there is no way to exercise a real commit
// without one.
func buildFormwork(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		var mod string
		mod, buildErr = moduleRoot()
		if buildErr != nil {
			return
		}
		dir, err := os.MkdirTemp("", "formwork-bin")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "formwork")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/formwork")
		cmd.Dir = mod
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildFailure{err: err, out: string(out)}
			return
		}
		builtBin = bin
	})
	if buildErr != nil {
		t.Fatalf("building the formwork binary: %v", buildErr)
	}
	return builtBin
}

type buildFailure struct {
	err error
	out string
}

func (b *buildFailure) Error() string { return b.err.Error() + "\n" + b.out }

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func TestInstalledShimGatesARealCommit(t *testing.T) {
	bin := buildFormwork(t)
	dir := repo(t)
	mustWriteAt(t, filepath.Join(dir, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWriteAt(t, filepath.Join(dir, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	// The Config is built here rather than loaded from the YAML above because
	// the rule-type registry is populated by blank imports in internal/cli, so
	// config.Load from this package cannot decode a `type:`. The BINARY loads
	// that YAML for real when git runs the hook — which is the half of this
	// test that matters. Both halves declare the same pre-commit lane over one
	// rule, which is all Install and Verify read.
	cfg := laneCfg("pre-commit")
	mustInstall(t, dir, cfg)
	wantWired(t, mustVerify(t, dir, cfg))

	// PATH preflight, fail closed. Without this the shim exits 127, the commit
	// is blocked for that reason, and the naive assertion ("commit blocked")
	// passes over a gate that never ran.
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	resolved, err := exec.LookPath("formwork")
	if err != nil {
		t.Fatalf("formwork is not on PATH, so the hook could only ever exit 127: %v", err)
	}
	if resolved != bin {
		t.Fatalf("PATH resolves formwork to %s, not the binary under test (%s)", resolved, bin)
	}

	// Direction 1: the rule fires and git refuses the commit.
	mustWriteAt(t, filepath.Join(dir, "bad.go"), "package a\n\nvar x = \"WIDGET\"\n")
	gitT(t, dir, "add", "-A")
	out, err := exec.Command("git", "-C", dir, "commit", "-m", "violating").CombinedOutput()
	if err == nil {
		t.Fatalf("git committed a violation the pre-commit lane forbids:\n%s", out)
	}
	if !strings.Contains(string(out), "no-widget") {
		t.Fatalf("the commit was blocked, but not BY THE RULE — exit status alone cannot tell a verdict from a missing binary:\n%s", out)
	}

	// Direction 2: the same repository accepts a clean commit.
	mustWriteAt(t, filepath.Join(dir, "bad.go"), "package a\n\nvar x = \"fine\"\n")
	gitT(t, dir, "add", "-A")
	if out, err := exec.Command("git", "-C", dir, "commit", "-m", "clean").CombinedOutput(); err != nil {
		t.Fatalf("the hook blocked a clean commit, so blocking proves nothing: %v\n%s", err, out)
	}
}

// TestVerifyAgreesWithGitAboutAShimGitWillNotRun is row 2 at git's altitude:
// git prints a hint and runs NOTHING, so the violation lands in a commit while
// the shim sits in place looking installed.
//
// TWO MODES, AND ONLY THE SECOND TESTS AN AGREEMENT. At 0644 every reading of
// the file agrees it is not executable, so a test pinning that mode alone claims
// agreement with git while exercising the case where agreement is free. At 0655
// (rw-r-xr-x) the readings diverge: two execute bits are set, and the OWNER —
// the user git asks access(X_OK) about — still cannot run it. That row is what
// makes this an agreement test, and it is where `mode&0o111` certified a shim
// git had already declined.
//
// 0111 is deliberately not a row: it removes read permission too, so the
// byte-compare fails first and the executable check is never reached.
func TestVerifyAgreesWithGitAboutAShimGitWillNotRun(t *testing.T) {
	bin := buildFormwork(t)
	for _, mode := range []os.FileMode{0o644, 0o655} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			if mode&0o100 == 0 && mode&0o111 != 0 && os.Geteuid() == 0 {
				t.Skip("root executes on any execute bit, so git would run this shim")
			}
			dir := repo(t)
			mustWriteAt(t, filepath.Join(dir, ".formwork", "formwork.yaml"),
				"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
			mustWriteAt(t, filepath.Join(dir, ".formwork", "rules", "r.yaml"),
				"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
			cfg := laneCfg("pre-commit")
			mustInstall(t, dir, cfg)
			if err := os.Chmod(filepath.Join(managedDir(dir), "pre-commit"), mode); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

			mustWriteAt(t, filepath.Join(dir, "bad.go"), "package a\n\nvar x = \"WIDGET\"\n")
			gitT(t, dir, "add", "-A")
			out, err := exec.Command("git", "-C", dir, "commit", "-m", "violating").CombinedOutput()
			if err != nil {
				t.Fatalf("this fixture is meant to show git NOT running the hook; it blocked the commit instead:\n%s", out)
			}
			// git let it through. Verify must not call that wiring intact.
			if probs := mustVerify(t, dir, cfg); len(probs) == 0 {
				t.Fatal("git committed the violation, and verify reported the hooks wired")
			}
		})
	}
}

func mustWriteAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
