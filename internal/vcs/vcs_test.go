package vcs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// initRepo makes a temp git repo with a configured identity and returns its
// path. Skips the test if git is unavailable.
// requireSymlinks skips only when the platform genuinely cannot make symlinks
// (Windows without the privilege, a filesystem that lacks them).
//
// The distinction matters because the test this guards is the ONLY thing
// standing between the tree and a regression that already shipped once in this
// fix's own history. A bare `t.Skipf` on any os.Symlink error would let that
// regression reland with `make verify` green — a leftover entry, a sandbox
// denial or a restricted runner would all read as "not supported". So the
// probe is separate from the assertion: if symlinks work here, a later failure
// to create one is a real failure.
func requireSymlinks(t *testing.T) {
	t.Helper()
	probe := t.TempDir()
	if err := os.Symlink(filepath.Join(probe, "target"), filepath.Join(probe, "probe")); err != nil {
		t.Skipf("symlinks unsupported on this platform/filesystem: %v", err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		// Pin the default explicitly: the quoteworthy-path tests (#96) are
		// only discriminating when git actually quotes, and a machine-global
		// core.quotepath=false would let the old line-split parser pass them
		// vacuously.
		{"config", "core.quotepath", "true"},
		// core.hooksPath is the same ambient-leak class and is deliberately NOT
		// pinned here, which is the opposite call from the line above (#295).
		// A pin would neutralise the leak and destroy the measurement with it:
		// TestHooksPathUnsetIsGitsDefaultHooksDir asks what git answers for a
		// repository that sets no hooks path, so a repository that sets one
		// answers a different question and passes whatever the ambient
		// configuration says. That fixture is also what main_test.go's seal
		// proof is pinned to, so pinning here would take the discriminating
		// power out of the proof as well. The leak is closed at the process
		// instead, in internal/vcs/gitseal, and
		// TestInitRepoLeavesCoreHooksPathUnsetSoTheFixturesStayDiscriminating
		// holds this decision.
	} {
		run(t, dir, args...)
	}
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStagedPathsListsAddedAndModifiedNotDeletedOrUnstaged(t *testing.T) {
	dir := initRepo(t)
	// Base commit with two files.
	write(t, dir, "keep.go", "package a\n")
	write(t, dir, "gone.go", "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")

	// Stage: add new, modify keep, delete gone. Leave an unstaged file.
	write(t, dir, "new/added.go", "package b\n")
	write(t, dir, "keep.go", "package a // changed\n")
	run(t, dir, "add", "new/added.go", "keep.go")
	// Distinct package clauses so the fixture reads as three independent edits.
	// NOT load-bearing, despite an earlier version of this comment saying so:
	// with identical contents git does pair the deletion with the addition
	// (R100), but neither assertion moves — ACMR reports the destination and
	// drops the source either way, and the AnyStatus test passes --no-renames
	// so detection never runs. Verified by making them identical: both stay
	// green (claim-auditor, 2026-08-19).
	run(t, dir, "rm", "-q", "gone.go")
	write(t, dir, "unstaged.go", "package c\n") // not added

	got, err := vcs.StagedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"keep.go", "new/added.go"} // sorted; deleted + unstaged excluded
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StagedPaths = %v, want %v", got, want)
	}
}

func TestRangePathsAcrossCommits(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "first")
	write(t, dir, "b.go", "package b\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "second")

	got, err := vcs.RangePaths(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths = %v, want %v", got, want)
	}
}

func TestStagedPathsFailsClosedOutsideRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	if _, err := vcs.StagedPaths(dir); err == nil {
		t.Fatal("expected error outside a git repo, got nil (fail-open hazard)")
	}
}

func TestEnsureTopLevelRejectsSubdir(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := vcs.EnsureTopLevel(sub); err == nil {
		t.Fatal("expected EnsureTopLevel to reject a subdirectory of the repo root")
	}
	if err := vcs.EnsureTopLevel(dir); err != nil {
		t.Fatalf("EnsureTopLevel(root) should pass, got %v", err)
	}
}

// TestEnsureTopLevelAcceptsRelativeRoot pins the case every other test in this
// file misses by always passing an absolute path: the DEFAULT root is ".", and
// it must be accepted when the process is sitting in the repository root.
//
// It was not. filepath.EvalSymlinks does not absolutise — EvalSymlinks(".")
// returns "." — so the comparison was always "<abs top>" != "." and every
// file-set run with the default -C died at exit 2. The generated git-hook shim
// passes no -C at all, so this made every installed hook fail on every commit
// (#142).
func TestEnsureTopLevelAcceptsRelativeRoot(t *testing.T) {
	dir := initRepo(t)
	t.Chdir(dir)

	if err := vcs.EnsureTopLevel("."); err != nil {
		t.Fatalf(`EnsureTopLevel(".") from the repo root must pass, got %v`, err)
	}
	if err := vcs.EnsureTopLevel("./"); err != nil {
		t.Fatalf(`EnsureTopLevel("./") from the repo root must pass, got %v`, err)
	}
}

// TestEnsureTopLevelRejectsRelativeSubdir is the other half: making relative
// roots work must not cost the guard its teeth. A relative path that resolves
// to a SUBDIRECTORY is still refused — the guard exists because a root that is
// not the top level makes git's repo-relative paths and the scan's
// root-relative paths disagree, and the intersection then silently matches
// nothing (a fail-open, not a cosmetic mismatch).
func TestEnsureTopLevelRejectsRelativeSubdir(t *testing.T) {
	dir := initRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if err := vcs.EnsureTopLevel("sub"); err == nil {
		t.Fatal(`EnsureTopLevel("sub") must be refused — relative must not mean unchecked`)
	}
	if err := vcs.EnsureTopLevel("./sub"); err == nil {
		t.Fatal(`EnsureTopLevel("./sub") must be refused`)
	}
}

// TestEnsureTopLevelRefusesSymlinkDotDotEscape pins the trap in fixing #142:
// the ORDER of filepath.Abs and filepath.EvalSymlinks is load-bearing, and
// getting it backwards reopens the exact fail-open this guard exists to stop.
//
// filepath.Abs calls Clean, which strips `x/..` LEXICALLY. The kernel does not
// — it follows `x` first, then applies `..` to wherever that landed. So for
// `link -> sub/deep`, `link/..` is `sub` physically but `.` lexically:
//
//	cd -P link/..                  -> <repo>/sub
//	EvalSymlinks("link/..")        -> sub          (agrees with the kernel)
//	EvalSymlinks(Abs("link/.."))   -> <repo>       (Abs already destroyed it)
//
// Absolutising first therefore reports the root AS the top-level when it is
// really a subdirectory. git -C link/.. runs in <repo>/sub and emits
// `sub/staged.go`; the scan walks the same directory but names it
// `staged.go`; the intersection is empty and the gate exits 0 over an
// unscanned changeset. Resolve first, absolutise second.
func TestEnsureTopLevelRefusesSymlinkDotDotEscape(t *testing.T) {
	dir := initRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	requireSymlinks(t)
	if err := os.Symlink(filepath.Join("sub", "deep"), filepath.Join(dir, "link")); err != nil {
		t.Fatalf("symlink creation failed on a platform that supports it: %v", err)
	}
	t.Chdir(dir)

	if err := vcs.EnsureTopLevel("link/.."); err == nil {
		t.Fatal(`EnsureTopLevel("link/..") must be refused — it resolves to <repo>/sub, not the top level`)
	}
}

// TestEnsureTopLevelAcceptsCaseVariantRoot pins the case a string comparison
// cannot get right: on a case-insensitive filesystem two different spellings
// name ONE directory, and `git rev-parse --show-toplevel` reports the on-disk
// spelling while the caller's -C (or os.Getwd, which returns $PWD verbatim)
// keeps whatever the shell used.
//
// A developer who cd's in as `myrepo` when the directory is `MyRepo` — which
// tab-completion, shell aliases and IDE terminals all produce — therefore had
// every `git commit` die at exit 2, which is #142 still live for them. The
// guard must compare IDENTITY (os.SameFile: device + inode), not spelling.
//
// Skips on a case-sensitive filesystem, where the variant is a different
// directory and refusing it is correct.
func TestEnsureTopLevelAcceptsCaseVariantRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "MyRepo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "Test"}} {
		run(t, dir, args...)
	}

	variant := filepath.Join(parent, "myrepo")
	if _, err := os.Stat(variant); err != nil {
		t.Skip("case-sensitive filesystem: the variant is a different directory")
	}

	if err := vcs.EnsureTopLevel(variant); err != nil {
		t.Fatalf("a case-variant spelling of the repo root names the same directory and must be accepted, got %v", err)
	}
}

// TestEnsureTopLevelAcceptsTrailingSpaceRoot and its sibling below pin the
// no-trimming contract this package already keeps everywhere else.
//
// diffPaths says it outright: "no per-record trimming, so trailing-space and
// control-character names survive intact", and TestTrackedPathsSurvivesTrailing
// SpaceName exists to stop a refactor reintroducing it. EnsureTopLevel was
// doing exactly that to the most load-bearing path in the package — the output
// of `rev-parse --show-toplevel` — via strings.TrimSpace, which strips the
// newline git appends AND any trailing space that is part of the directory
// name.
//
// A repository root may legally end in a space. Trimming it produces a
// different path, which then fails to stat: a healthy repo, spelled correctly,
// refused with an error blaming git for a path this function damaged.
func TestEnsureTopLevelAcceptsTrailingSpaceRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "foo ") // trailing space, legal on unix
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("filesystem rejects a trailing-space directory name: %v", err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "Test"}} {
		run(t, dir, args...)
	}

	if err := vcs.EnsureTopLevel(dir); err != nil {
		t.Fatalf("a repository root whose name ends in a space is still the root, got %v", err)
	}
}

// TestEnsureTopLevelRefusesWhitespaceMangledTopLevel is the fail-open the same
// trimming allowed, and it is the one that mattered: trimming can land the
// compared path on a DIFFERENT directory that happens to exist.
//
// Repo root is `foo ` (trailing space); a sibling symlink `foo` — the trimmed
// spelling — points at `foo /sub`. Both stats then resolve to one inode, so an
// identity check says "same directory" for a root that is a SUBDIRECTORY. git
// enumerates repo-relative `src/bad.txt` while the scan under `sub` names it
// `bad.txt`; the intersection is empty and every rule reports OK at exit 0 over
// a staged violation.
func TestEnsureTopLevelRefusesWhitespaceMangledTopLevel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	requireSymlinks(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "foo ")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Skipf("filesystem rejects a trailing-space directory name: %v", err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "Test"}} {
		run(t, dir, args...)
	}
	if err := os.Symlink(filepath.Join("foo ", "sub"), filepath.Join(parent, "foo")); err != nil {
		t.Fatalf("symlink creation failed on a platform that supports it: %v", err)
	}

	// The trimmed spelling resolves to a subdirectory of the repo, not its root.
	if err := vcs.EnsureTopLevel(filepath.Join(parent, "foo")); err == nil {
		t.Fatal("a root resolving to a subdirectory must be refused, whatever whitespace is involved")
	}
}

// TestEnsureTopLevelRefusesSubdirWhenIdentityCannotDistinguish pins that the
// guard does not rest solely on os.SameFile.
//
// SameFile has no error path on unix — it is `Dev == Dev && Ino == Ino`
// (os/types_unix.go) — and on Windows GetFileInformationByHandle SUCCEEDS with
// all-zero file indices on filesystems that do not support file IDs. On any
// such substrate (SMB/9p/FUSE, container bind mounts, overlayfs without xino,
// exFAT) every directory compares equal, so a degenerate filesystem makes an
// identity-only guard answer "same" for everything: it does not fail closed,
// it goes silently inert.
//
// So the load-bearing check asks git, in git's own frame, whether the root IS
// the top level (`rev-parse --show-prefix` is empty iff it is). This test
// forces the identity check to the degenerate answer and asserts a
// subdirectory is still refused.
func TestEnsureTopLevelRefusesSubdirWhenIdentityCannotDistinguish(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	restore := vcs.SetSameFileForTest(func(a, b os.FileInfo) bool { return true })
	t.Cleanup(restore)

	if err := vcs.EnsureTopLevel(sub); err == nil {
		t.Fatal("a subdirectory must stay refused even where the filesystem cannot distinguish directories")
	}
	if err := vcs.EnsureTopLevel(dir); err != nil {
		t.Fatalf("the real root must still be accepted, got %v", err)
	}
}

func TestRangePathsEmptyRangeErrors(t *testing.T) {
	dir := initRepo(t)
	if _, err := vcs.RangePaths(dir, "   "); err == nil {
		t.Fatal("expected error for empty range")
	}
}

func TestTrackedPathsListsTrackedExcludesUntracked(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "sub/b.go", "package b\n")
	run(t, dir, "add", "a.go", "sub/b.go")
	run(t, dir, "commit", "-q", "-m", "base")
	// An untracked file on disk must NOT appear in the tracked set — that is the
	// property the whole-tree-invariant corpus relies on (#4).
	write(t, dir, "untracked.go", "package c\n")

	got, err := vcs.TrackedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "sub/b.go"} // sorted, forward-slashed; untracked excluded
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedPaths = %v, want %v", got, want)
	}
}

// TestTrackedUnderSubdirIsSubdirRelative pins the property TrackedUnder exists
// for: unlike TrackedPaths it does not require root to be the repository
// top-level, and its paths come back relative to root — the same frame lint's
// scan uses — because git ls-files reports relative to its cwd.
func TestTrackedUnderSubdirIsSubdirRelative(t *testing.T) {
	repo := initRepo(t)
	write(t, repo, "sub/inner/a.go", "package a\n")
	write(t, repo, "top.go", "package top\n")
	run(t, repo, "add", "-A")

	got, err := vcs.TrackedUnder(filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"inner/a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder(sub) = %v, want %v (subdir-relative, top.go excluded)", got, want)
	}
}

// TestTrackedUnderNotARepoErrors pins fail-closed: outside any repository the
// caller gets an error, never an empty (vacuously clean) tracked set. Note
// the boundary this test's TempDir silently depends on: git -C resolves to
// the NEAREST ANCESTOR repo, so a non-repo root under an unrelated outer repo
// answers for that repo (empty-under-root = truthful vacuous OK) — that is
// what makes lint-over-subdir-corpora (#89) work, and it is documented on
// TrackedUnder rather than being an accident.
func TestTrackedUnderNotARepoErrors(t *testing.T) {
	if _, err := vcs.TrackedUnder(t.TempDir()); err == nil {
		t.Fatal("TrackedUnder outside a git repository must error, not return an empty tracked set")
	}
}

// TestTrackedUnderSurvivesQuoteworthyPaths pins the core.quotePath hazard
// (#90 review): with default config git ls-files C-quotes any path holding a
// byte >= 0x80 ("na\303\257ve.ts"), and a line-split parser returns that
// quoted string — which can never match a walk path, silently passing the
// exact bypass the caller exists to catch. -z output is never quoted.
func TestTrackedUnderSurvivesQuoteworthyPaths(t *testing.T) {
	repo := initRepo(t)
	write(t, repo, "scratch/naïve.ts", "export const x = 1\n")
	run(t, repo, "add", "-f", "-A")

	got, err := vcs.TrackedUnder(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scratch/naïve.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder = %v, want the unquoted literal path %v", got, want)
	}
}

// TestTrackedUnderExcludesSubmoduleGitlinks pins the mode-160000 filter: a
// gitlink is a pointer, not a file of this repository — reporting it as
// "tracked but hidden" would tell the operator to delete a legitimate
// vendored-submodule registration. Files INSIDE a submodule can never enter
// the outer index (verified in the #90 plan review), so excluding gitlinks
// reopens no bypass. The gitlink is fabricated via update-index --cacheinfo
// (no real submodule, no network; gitlink object existence is not validated).
func TestTrackedUnderExcludesSubmoduleGitlinks(t *testing.T) {
	repo := initRepo(t)
	write(t, repo, "kept.go", "package kept\n")
	run(t, repo, "add", "-A")
	run(t, repo, "update-index", "--add", "--cacheinfo",
		"160000,0000000000000000000000000000000000000001,scratch/sub")

	got, err := vcs.TrackedUnder(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kept.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder = %v, want gitlink excluded: %v", got, want)
	}
}

// TestGetConfigBoolNormalizesSpellings pins the gate hazard (#90 review):
// `git config --get` returns the value AS SPELLED, so a truthy spelling like
// "yes" — which git's own boolean parser honors — would read as not-"true"
// and silently flip any gate comparing the raw string. --type=bool
// normalizes through git's parser.
func TestGetConfigBoolNormalizesSpellings(t *testing.T) {
	repo := initRepo(t)
	for spelling, want := range map[string]bool{
		"yes": true, "on": true, "1": true, "True": true,
		"no": false, "off": false, "0": false,
	} {
		run(t, repo, "config", "test.flag", spelling)
		got, err := vcs.GetConfigBool(repo, "test.flag")
		if err != nil {
			t.Fatalf("%q: %v", spelling, err)
		}
		if got != want {
			t.Errorf("GetConfigBool(%q) = %v, want %v", spelling, got, want)
		}
	}
	if _, err := vcs.GetConfigBool(repo, "test.unset"); err == nil {
		t.Fatal("unset key must error, matching GetConfig's contract")
	}
}

// The three quoteworthy-path tests pin #96: with default core.quotePath git
// C-quotes any path holding a byte >= 0x80, and a line-split parser returns
// the quoted spelling — a string that can never intersect the scanner's
// paths, so the file is silently unscanned in --staged/--range modes and
// invisible to whole-tree-invariant evaluation. Owed: the literal path.

func TestStagedPathsSurvivesQuoteworthyPaths(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "base.go", "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "naïve.ts", "export const x = 1\n")
	run(t, dir, "add", "naïve.ts")

	got, err := vcs.StagedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"naïve.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StagedPaths = %v, want the unquoted literal %v", got, want)
	}
}

func TestRangePathsSurvivesQuoteworthyPaths(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "base.go", "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "naïve.ts", "export const x = 1\n")
	run(t, dir, "add", "naïve.ts")
	run(t, dir, "commit", "-q", "-m", "add naive")

	got, err := vcs.RangePaths(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"naïve.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths = %v, want the unquoted literal %v", got, want)
	}
}

func TestTrackedPathsSurvivesQuoteworthyPaths(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "naïve.ts", "export const x = 1\n")
	run(t, dir, "add", "-A")

	got, err := vcs.TrackedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"naïve.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedPaths = %v, want the unquoted literal %v", got, want)
	}
}

// TestRangePathsSurvivesPathspecSeparatorInRange pins the flag-placement
// hazard (#96 review, a blocker introduced by the first -z conversion): the
// range string is split into git args verbatim, so "A..B -- ." is a legal
// caller shape — and a trailing -z lands AFTER the -- separator, where git
// swallows it as a pathspec (exit 0, no error) and emits newline-separated,
// quoted output that the NUL split fuses into one garbage record: an empty
// intersection, a green gate over an unscanned changeset. -z must precede
// the caller-supplied revs.
func TestRangePathsSurvivesPathspecSeparatorInRange(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "base.go", "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "naïve.ts", "export const x = 1\n")
	run(t, dir, "add", "naïve.ts")
	run(t, dir, "commit", "-q", "-m", "add naive")

	got, err := vcs.RangePaths(dir, "HEAD~1..HEAD -- .")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"naïve.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths with a -- pathspec = %v, want %v", got, want)
	}
}

// TestTrackedPathsSurvivesTrailingSpaceName pins the parser's no-trimming
// contract (#97 review): the doc comment promises trailing-space names
// survive, but every other test here uses whitespace-free names, so a
// refactor reintroducing per-record TrimSpace would pass the package green
// while a committed "notes.md " is trimmed to a spelling that never
// intersects the scanner's paths — the #96 silent-unscan shape back again.
func TestTrackedPathsSurvivesTrailingSpaceName(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "notes.md ", "trailing space in the name\n")
	run(t, dir, "add", "-f", "-A")

	got, err := vcs.TrackedPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"notes.md "}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedPaths = %q, want the untrimmed literal %q", got, want)
	}
}

// TestAnyStatusHelpersReportDeletionsAndRenameSources covers the two helpers
// #147 added, which no test referenced at all — their behaviour was pinned only
// through `formwork scope`'s output, so nothing said what the seam itself owes.
//
// Both halves of "any status" are asserted here. The deletion is what dropping
// --diff-filter=ACMR buys. The rename SOURCE is what --no-renames buys, and it
// is a separate mechanism: with rename detection on, git reports one path — the
// destination — so the source is never emitted for a filter to admit. Keeping
// them in one test is deliberate; the scope-level symptom is identical
// (go_changed=false) and only this level tells the two causes apart.
//
// Rename detection is left at git's default rather than forced on, because the
// point is that this answer must not depend on operator config: the same index
// under `diff.renames=false` already reported the source.
func TestAnyStatusHelpersReportDeletionsAndRenameSources(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "gone.go", "package gone\n")
	write(t, dir, "src/api.go", "package src\n")
	// git mv refuses a destination directory that does not exist, so docs/ has
	// to be in the base commit rather than created by the move.
	write(t, dir, "docs/.keep", "")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")

	// Distinct package clauses so the fixture reads as three independent edits.
	// NOT load-bearing, despite an earlier version of this comment saying so:
	// with identical contents git does pair the deletion with the addition
	// (R100), but neither assertion moves — ACMR reports the destination and
	// drops the source either way, and the AnyStatus test passes --no-renames
	// so detection never runs. Verified by making them identical: both stay
	// green (claim-auditor, 2026-08-19).
	run(t, dir, "rm", "-q", "gone.go")
	run(t, dir, "mv", filepath.Join("src", "api.go"), filepath.Join("docs", "api.md"))
	write(t, dir, "added.go", "package added\n")
	run(t, dir, "add", "-A")

	want := []string{"added.go", "docs/api.md", "gone.go", "src/api.go"}

	got, err := vcs.StagedPathsAnyStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StagedPathsAnyStatus = %q, want %q", got, want)
	}

	run(t, dir, "commit", "-q", "-m", "change")

	got, err = vcs.RangePathsAnyStatus(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePathsAnyStatus = %q, want %q", got, want)
	}
}
