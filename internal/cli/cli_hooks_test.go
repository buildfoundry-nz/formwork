package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CLI-level `formwork hooks` tests. Split out of cli_test.go when it reached the
// repo's own 750-line hard cap (file-size-vendor-cap) — the cure on that rule is
// "split the file", never widen the cap, because consumers that vendor this
// source enforce it downstream.
//
// Package-level helpers (runCLI, mustWrite, gitInit) live in cli_test.go; the
// no-t.Parallel() rule documented there applies to this file too.

func TestHooksInstallThenVerify(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	// The lane needs a rule: a git-hook lane selecting nothing is refused at
	// install, because the shim it generates would abort every commit (#151).
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)
	code, out, errOut := runCLI(t, "hooks", "install", "-C", root)
	if code != 0 {
		t.Fatalf("install exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "installed git hooks: pre-commit") {
		t.Fatalf("install output: %s", out)
	}
	code2, out2, _ := runCLI(t, "hooks", "verify", "-C", root)
	if code2 != 0 || !strings.Contains(out2, "hooks wired") {
		t.Fatalf("verify exit %d\n%s", code2, out2)
	}
}

// The rule here is load-bearing, and its absence made this test vacuous for one
// commit. Without it the lane selects nothing, so Verify returns the empty-lane
// problem before it ever reads core.hooksPath or the shim files — deleting BOTH
// wiring branches from hooks.Verify left this test green. It is the only
// end-to-end coverage of the not-yet-installed verdict, so it must fail for the
// wiring reason and no other; the negative assertion below pins that.
func TestHooksVerifyFailsExit1BeforeInstall(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)
	code, out, _ := runCLI(t, "hooks", "verify", "-C", root)
	if code != 1 {
		t.Fatalf("verify before install should exit 1, got %d\n%s", code, out)
	}
	// Was `core.hooksPath`, the config string verify used to compare against a
	// constant. #146 makes the verdict file-level at the directory git names,
	// so the wiring reason now reads as the absent shim — the same assertion
	// against the check that replaced it, not a weakened one.
	if !strings.Contains(out, "no shim") {
		t.Fatalf("must fail for the WIRING reason, not an unrelated problem:\n%s", out)
	}
	if strings.Contains(out, "selects no rules") {
		t.Fatalf("the fixture lost its rule, so this test no longer exercises wiring:\n%s", out)
	}
}

// A directory that is not a git repository is an ENGINE fault, not a wiring
// verdict. Before #146 it exited 1 with "core.hooksPath is not set (run:
// formwork hooks install)" — a layout diagnosis invented from a tool failure,
// and advice that would not have helped.
func TestHooksVerifyOutsideARepositoryExits2(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	// Deliberately no gitInit.
	code, out, errOut := runCLI(t, "hooks", "verify", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (git fault)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "git") {
		t.Errorf("stderr should name the git failure:\n%s", errOut)
	}
	if strings.Contains(out, "hooks wired") {
		t.Errorf("verify must not certify anything here:\n%s", out)
	}
}

// A stale git-hook lane must not cost the operator the hooks that are healthy.
// The first cut of the empty-lane guard refused the WHOLE install, so a repo
// with a working pre-commit and a dead pre-push got neither — and bootstrap
// scripts run `hooks install` and continue, leaving the developer committing
// with no gate at all where they previously had one.
func TestHooksInstallWiresHealthyLanesDespiteADeadOne(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    tags: [go]\n  pre-push:\n    tags: [sql]\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    tags: [go]\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)

	code, out, errOut := runCLI(t, "hooks", "install", "-C", root)
	if code != 2 {
		t.Fatalf("a dead lane must still be loud: exit = %d\n%s\n%s", code, out, errOut)
	}
	// Loud AND protecting: the healthy hook is installed and reported.
	if !strings.Contains(out, "installed git hooks: pre-commit") {
		t.Errorf("the healthy lane lost its hook because a different lane was stale:\n%s", out)
	}
	if !strings.Contains(errOut, "pre-push") {
		t.Errorf("the dead lane must be named:\n%s", errOut)
	}
	if _, err := os.Stat(filepath.Join(root, ".formwork", "hooks", "pre-commit")); err != nil {
		t.Errorf("no pre-commit shim on disk: %v", err)
	}
	// And no shim for the dead lane — one that ran zero rules would abort every
	// push with nothing the developer could act on.
	if _, err := os.Stat(filepath.Join(root, ".formwork", "hooks", "pre-push")); err == nil {
		t.Error("wrote a shim for a lane that selects no rules")
	}
}

// --- the contrast pair: install has two refusals, and they are not alike -----
//
// READ THESE TWO TOGETHER. Both exit 2. One changes nothing at all; the other
// sets core.hooksPath and leaves a working shim on disk. Apart, each looks like
// an ordinary test and the boundary between them is invisible — and losing that
// boundary is the regression two reviewers independently called the likeliest in
// this work, in both directions: generalising "a refusal changes nothing" takes
// the healthy lane's gate away from a developer over a stale one, and narrowing
// it lets a pre-flight refusal write half an install.
//
// The pre-flight half also has a stronger sibling in internal/hooks, where
// TestInstallRefusesOverTheHooksGitIsRunning asserts on a byte-level snapshot of
// the whole repository. This one is at the command's altitude: exit code, what
// git's config says afterwards, and what is on disk.

func TestHooksInstallPreflightRefusalChangesNothing(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)
	// A hook git is running right now. Setting core.hooksPath overrides the whole
	// directory it is in, so install refuses (#146 D2).
	theirs := filepath.Join(root, ".git", "hooks", "pre-commit")
	mustWrite(t, theirs, "#!/bin/sh\necho theirs\n")
	if err := os.Chmod(theirs, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runCLI(t, "hooks", "install", "-C", root)
	if code != 2 {
		t.Fatalf("a refusal is exit 2, got %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(out, "installed git hooks") {
		t.Errorf("a pre-flight refusal reported an install:\n%s", out)
	}
	if v, ok := repoHooksPath(t, root); ok {
		t.Errorf("core.hooksPath was set to %q by a run that refused", v)
	}
	if _, err := os.Stat(filepath.Join(root, ".formwork", "hooks")); err == nil {
		t.Error("a pre-flight refusal created the managed hooks directory")
	}
}

func TestHooksInstallEmptyLaneRefusalStillWiresTheHealthyLane(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    tags: [go]\n  pre-push:\n    tags: [sql]\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    tags: [go]\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)

	code, out, errOut := runCLI(t, "hooks", "install", "-C", root)
	if code != 2 {
		t.Fatalf("a stale lane is still loud: exit = %d\n%s\n%s", code, out, errOut)
	}
	// The half the older sibling test does not assert: the config IS written, so
	// the healthy shim is not merely on disk but wired.
	if v, ok := repoHooksPath(t, root); !ok || v != ".formwork/hooks" {
		t.Errorf("core.hooksPath = %q (set=%v), so the healthy lane's shim is not wired", v, ok)
	}
	if _, err := os.Stat(filepath.Join(root, ".formwork", "hooks", "pre-commit")); err != nil {
		t.Errorf("the healthy lane lost its shim because a different lane was stale: %v", err)
	}
}

// --- --override-global, at the command surface -------------------------------
//
// internal/hooks already pins what the flag MEANS (preflight_test.go: the D7
// refusal, the repo-local install it authorises, and the three refusals it does
// not unlock). What no test there can reach is whether typing it on the command
// line arrives at all: `Install` took the parameter while the call site passed a
// hard-coded false, and every hooks-package test stayed green throughout. These
// tests run the real argv through cli.Run, so the wiring itself is what is under
// test — in both directions, because a call site hard-coded either way still
// passes one of them.

// globalHooksPathFixture gives this test process its own HOME holding one
// `.gitconfig` that declares a core.hooksPath, and registers the assertion that
// the run left that file byte-identical. Same shape, and the same reasoning, as
// internal/hooks' globalHooksPath: it declares a wiring wider than this
// repository through the file such a wiring really lives in, rather than through
// GIT_CONFIG_GLOBAL — which install now refuses wherever it moves git's answer,
// so a fixture built on it would test the refusal of the fixture.
func globalHooksPathFixture(t *testing.T, value string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".gitconfig")
	body := "[core]\n\thooksPath = " + value + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(func() {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("git's global config is unreadable after the run: %v", err)
			return
		}
		if string(got) != body {
			t.Errorf("formwork wrote to git's global config\nbefore: %q\nafter:  %q", body, got)
		}
	})
}

// hooksLaneRepo builds a git repository with one git-hook lane and one rule the
// lane selects — the minimum an install can succeed from.
func hooksLaneRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)
	return root
}

func TestHooksInstallRefusesAWiderScopeWiringWithoutTheFlag(t *testing.T) {
	root := hooksLaneRepo(t)
	globalHooksPathFixture(t, ".husky")

	code, out, errOut := runCLI(t, "hooks", "install", "-C", root)
	if code != 2 {
		t.Fatalf("a refusal is exit 2, got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "--override-global") {
		t.Errorf("the refusal must name the escape:\n%s", errOut)
	}
	if strings.Contains(out, "installed git hooks") {
		t.Errorf("a refusal reported an install:\n%s", out)
	}
	if v, ok := repoHooksPath(t, root); ok {
		t.Errorf("core.hooksPath was set to %q by a run that refused", v)
	}
}

func TestHooksInstallOverrideGlobalFlagInstalls(t *testing.T) {
	root := hooksLaneRepo(t)
	globalHooksPathFixture(t, ".husky")

	code, out, errOut := runCLI(t, "hooks", "install", "--override-global", "-C", root)
	if code != 0 {
		t.Fatalf("--override-global did not reach Install: exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "installed git hooks: pre-commit") {
		t.Errorf("install output: %s", out)
	}
	// Repo-local, which is the whole content of the override: the global file's
	// bytes are asserted by globalHooksPathFixture's cleanup.
	if v, ok := repoHooksPath(t, root); !ok || v != ".formwork/hooks" {
		t.Errorf("core.hooksPath in this repository = %q (set=%v), want .formwork/hooks", v, ok)
	}
}

// --- the ambient git configuration environment, at the command surface -------
//
// internal/hooks pins WHAT the two commands decide (gitenv_test.go). What only
// this reaches is the exit status each decision carries, and the two are
// deliberately different: install's is a refusal (2, an engine/config error,
// nothing written), verify's is a finding about the repository (1). A shared
// guard returning one shared code would be wrong at one of the two sites.
func TestHooksUnderAnEnvironmentThatMovesGitsAnswer(t *testing.T) {
	root := hooksLaneRepo(t)
	if code, out, errOut := runCLI(t, "hooks", "install", "-C", root); code != 0 {
		t.Fatalf("baseline install exit %d\n%s\n%s", code, out, errOut)
	}
	if code, out, errOut := runCLI(t, "hooks", "verify", "-C", root); code != 0 {
		t.Fatalf("baseline verify exit %d\n%s\n%s", code, out, errOut)
	}
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.hooksPath'='.husky'")

	code, out, errOut := runCLI(t, "hooks", "install", "-C", root)
	if code != 2 {
		t.Fatalf("a refusal is exit 2, got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "GIT_CONFIG_PARAMETERS") {
		t.Errorf("the refusal must name what to unset:\n%s", errOut)
	}
	if strings.Contains(out, "installed git hooks") {
		t.Errorf("a refusal reported an install:\n%s", out)
	}

	code, out, errOut = runCLI(t, "hooks", "verify", "-C", root)
	if code != 1 {
		t.Fatalf("a wiring verdict is exit 1, got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "hooks wired") {
		t.Errorf("verify certified from an environment that moved git's answer:\n%s", out)
	}
}

// `verify` takes no --override-global: it changes nothing, so there is nothing
// for an override to authorise, and accepting the flag would advertise a power
// the command does not have. Rejected loudly (exit 2), never ignored.
func TestHooksVerifyRejectsOverrideGlobal(t *testing.T) {
	root := hooksLaneRepo(t)
	if code, out, errOut := runCLI(t, "hooks", "install", "-C", root); code != 0 {
		t.Fatalf("install exit %d\n%s\n%s", code, out, errOut)
	}
	// Without the flag this same repository verifies clean, so the exit 2 below
	// can only be the flag.
	if code, out, _ := runCLI(t, "hooks", "verify", "-C", root); code != 0 {
		t.Fatalf("baseline verify exit %d\n%s", code, out)
	}

	code, out, errOut := runCLI(t, "hooks", "verify", "--override-global", "-C", root)
	if code != 2 {
		t.Fatalf("an unknown flag is exit 2, got %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "hooks wired") {
		t.Errorf("verify ran with a flag it does not define:\n%s", out)
	}
	if !strings.Contains(errOut, "override-global") {
		t.Errorf("stderr should name the flag it rejected:\n%s", errOut)
	}
}

// repoHooksPath reports the repository's own core.hooksPath and whether it is
// set at all. `--local` scope, not the effective value: the question is what
// this run did to this repository, and a value inherited from the machine would
// answer a different one.
func repoHooksPath(t *testing.T, root string) (string, bool) {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "config", "--local", "--get", "core.hooksPath").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// TestHooksVerifyGatesOnEngine is finding 6: hooks verify must not bypass the
// engine gate — an under-version binary must not report "hooks wired" for a
// config it does not support.
func TestHooksVerifyGatesOnEngine(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "")
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nengine: \">=99.0.0\"\nlanes:\n  pre-commit:\n    all: true\n")
	code, _, errOut := runCLI(t, "hooks", "verify", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "engine") {
		t.Fatalf("stderr should mention engine, got:\n%s", errOut)
	}
}
