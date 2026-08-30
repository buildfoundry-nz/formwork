package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitOut runs git for its STDOUT, where gitRun runs it for its exit status.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// #158: `check --staged` takes its file LIST from the index and its CONTENT
// from the working tree. A file staged and then removed from the worktree is
// therefore named by git, produced by no walk, and evaluated by no rule — and
// the run reported "1/1 rules passed" at exit 0 while the staged bytes went on
// to commit. Every row below is about that gap: the paths git named must all be
// accounted for, or the run must refuse to speak for them.
//
// The four legitimate-empty rows are as load-bearing as the refusal rows. A
// guard that made this loud by making everything loud would still satisfy the
// refusal half, and `check --staged` is what the pre-commit shim runs.

// stagedThenDeletedRepo stages the whole tree of zeroFilesRepo and then removes
// src/bad.go from the WORKTREE without staging the removal — so the index still
// carries `const x = "WIDGET"` and the disk carries nothing.
func stagedThenDeletedRepo(t *testing.T, envelope string) string {
	t.Helper()
	root := zeroFilesRepo(t, envelope, "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	mustRemove(t, filepath.Join(root, "src", "bad.go"))
	return root
}

func TestCheckStagedThenDeletedFromWorktreeIsRefused(t *testing.T) {
	root := stagedThenDeletedRepo(t, "version: 1\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the staged content commits unchecked\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "src/bad.go") {
		t.Errorf("stderr must name every unaccounted path:\n%s", errOut)
	}
	if !strings.Contains(errOut, "not present in the working tree") {
		t.Errorf("stderr must give the arrival reason:\n%s", errOut)
	}
	// The --staged-only half of the cure. It says what the operator is about to
	// do, not just what the run failed to do, and it is true only here: under
	// --range nothing is being committed.
	if !strings.Contains(errOut, "would commit unchecked") {
		t.Errorf("--staged must say the staged content is what commits:\n%s", errOut)
	}
}

// The placement guard, on its own so that nothing short-circuits it. It lived
// inside the row above, behind a t.Fatalf on the exit code — which meant the
// mutation that makes the run render a verdict (dropping the refusal) never
// reached the assertion, and the guard proved nothing. A refusal that arrives
// after a rendered "1/1 rules passed" has already told the operator the opposite
// of what the exit code means.
func TestCheckStagedRefusalIsNotPrecededByAVerdict(t *testing.T) {
	root := stagedThenDeletedRepo(t, "version: 1\n")
	_, out, _ := runCLI(t, "check", "-C", root, "--staged")
	if strings.Contains(out, "rules passed") {
		t.Errorf("a refusal must not be preceded by a rendered verdict:\n%s", out)
	}
}

// The same defect under --range. The file list comes from a commit range and
// the content still comes from the worktree, so a path deleted (unstaged) after
// the range's endpoint is named and never read.
func TestCheckRangeNamingAWorktreeDeletionIsRefused(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "src", "later.go"), "const y = 1\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "second")
	mustRemove(t, filepath.Join(root, "src", "later.go"))

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "src/later.go") {
		t.Errorf("stderr must name the path:\n%s", errOut)
	}
	if !strings.Contains(errOut, "--range HEAD~1..HEAD") {
		t.Errorf("stderr must name the flag that requested it:\n%s", errOut)
	}
	// The commit-time half of the cure belongs to --staged alone. Printing it
	// here would tell a CI operator their range run is about to commit something.
	if strings.Contains(errOut, "would commit unchecked") {
		t.Errorf("--range commits nothing; that cure is false here:\n%s", errOut)
	}
	// It still owes a cure. The blast radius of this mode is local development
	// with a dirty tree, where "the scan never produced it" alone leaves the
	// developer guessing at what to do about it.
	if !strings.Contains(errOut, "restore them") {
		t.Errorf("--range must name the cure for a path that is not on disk:\n%s", errOut)
	}
}

// ---------------------------------------------------------------------------
// The four legitimate empties. Each must stay exit 0 AND stay quiet.
// ---------------------------------------------------------------------------

// Two of the four are already pinned, by exit code alone — which is a complete
// assertion here, because this guard has exactly two outcomes and the loud one
// is exit 2. `TestCheckStagedNothingStagedStaysExit0` (nothing staged) and
// `TestCheckStagedOnlyFormworkStagedStaysExit0` (a config-only commit) are in
// cli_zerofiles_test.go; the two below are the ones nothing covered.

func TestCheckRangeEmptyChangesetStaysExit0(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "never produced") {
		t.Errorf("an empty range is not an unaccounted path:\n%s", errOut)
	}
}

func TestCheckStagedOrdinaryChangesetStaysExit0(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const x = 1\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "src", "ok.go"), "const x = 2\n")
	gitRun(t, root, "add", "-A")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "1 path(s) requested by --staged, 1 file(s) scanned") {
		t.Errorf("every requested path was scanned; the summary must say so:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// The carve-out: entries the WHOLE-TREE walk also declines to produce.
// ---------------------------------------------------------------------------

// A staged symlink whose name carries no source extension is skipped by the
// walk everywhere, not just here — `formwork check` over the whole tree reads it
// no more than `--staged` does. This guard exists to make a file-set run check
// what a whole-tree run would have checked of these paths; making it STRICTER
// than the whole-tree run would refuse every commit that adds a symlink while
// changing nothing about what gets enforced.
//
// (The dangerous subset — a symlink whose own name ends in a source extension —
// is refused one layer down by scan.WalkWith, which errors the walk out before
// this accounting runs; internal/scan/scan.go's non-regular branch.)
func TestCheckStagedSymlinkIsNotRefusedAsAbsent(t *testing.T) {
	root := zeroFilesRepo(t, "version: 1\n", "{include: ['**/*.go']}")
	gitInit(t, root)
	if err := os.Symlink("src", filepath.Join(root, "alias.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gitRun(t, root, "add", "-A")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 1 {
		// exit 1: src/bad.go is staged and genuinely violates. The point is that
		// the symlink did not turn that verdict into a refusal.
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "alias.txt") {
		t.Errorf("a non-regular entry the walk skips repo-wide is not an unaccounted path:\n%s", errOut)
	}
}

// A submodule enters the changeset as a GITLINK: `git diff --name-only` names
// the submodule path, and on disk that path is a directory. The walk produces
// directories for nobody, so an accounting that judged only "absent from the
// scan" would refuse every commit that touches a submodule.
//
// The gitlink is written with update-index --cacheinfo rather than by adding a
// real submodule, so the test needs no network and no second repository.
func TestCheckStagedGitlinkIsNotRefusedAsAbsent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	if err := os.MkdirAll(filepath.Join(root, "vendor", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, root, "rev-parse", "HEAD")
	gitRun(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/sub")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "vendor/sub") {
		t.Errorf("a gitlink is a pointer, not a file this repo scans:\n%s", errOut)
	}
}
