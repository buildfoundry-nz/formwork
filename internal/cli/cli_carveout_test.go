package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The carve-outs in requestedButAbsent decide which absences are NOT a coverage
// gap. Each one is a hole in a fail-closed guard, so each one is bounded here
// from both sides: the entry it must excuse, and the entry that wears the same
// costume and must not be.
//
// Two rows below were fail-opens of the very defect #158 closes — a path that
// read as a pass when it owed a failure — found in review of the first cut.

// carveoutRepo builds a repo whose single rule covers EVERY file, so a fixture
// can be named anything at all and still be governed.
func carveoutRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: WIDGET}\n")
	return root
}

// FIX A. scan.UnderBuiltinSkip matches ANY path segment, the leaf included, but
// WalkWith consults skipDirs only in its d.IsDir() branch — so a REGULAR FILE
// named `.formwork` is scanned and enforced on like any other. Excusing it here
// contradicted the walk: stage such a file, delete it from the worktree, and
// `--staged` reported "1/1 rules passed" at exit 0 while the blob committed.
//
// scan.NotScannedBy already writes `!leaf && skipDirs[seg]` for exactly this
// reason (#119 third-pass finding 2). This guard reached for the coarser helper
// and reintroduced the contradiction #119 removed. git rejects a `.git` path
// component outright, so `.formwork` is the only reachable spelling.
func TestCheckStagedRegularFileNamedFormworkIsRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "sub", ".formwork"), "const x = \"WIDGET\"\n")
	gitInit(t, root)

	// The control: the walk really does scan it, so there really is coverage to
	// lose. Without this the row could pass over a file nothing governs.
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("whole-tree exit = %d, want 1 — a regular file named .formwork is scanned\n%s", code, out)
	}

	gitRun(t, root, "add", "-A")
	mustRemove(t, filepath.Join(root, "sub", ".formwork"))
	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the staged blob commits unchecked\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "sub/.formwork") {
		t.Errorf("stderr must name the path:\n%s", errOut)
	}
}

// The other side of Fix A, and the reason the predicate is ancestor-only rather
// than simply deleted: a path UNDER a .formwork directory is genuinely never
// scannable, and every commit that edits formwork's own rules stages nothing
// else. `TestCheckStagedOnlyFormworkStagedStaysExit0` pins the exit code; this
// pins that the requested COUNT stays honest too, which is the half #160 got
// wrong in the same way.
func TestCheckStagedFormworkAncestorIsStillExcused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "a.txt"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r2.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: BANANA}\n")
	gitRun(t, root, "add", "-A")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "0 path(s) requested") {
		t.Errorf("a path under .formwork was never scannable — counting it invents a gap:\n%s", out)
	}
}

// The COUNT half of Fix A, which needs its own row because the refusal half
// exits 2 and renders no summary at all. Stage the same regular `.formwork` file
// WITHOUT deleting it: now it is scanned, so it must also be counted. Under the
// leaf-matching predicate the headline read "0 path(s) requested, 1 file(s)
// scanned" — arithmetic that cannot be true, and the requested-vs-scanned
// indicator silenced for the one path it had something to say about.
func TestCheckStagedRegularFileNamedFormworkIsCounted(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "sub", ".formwork"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "1 path(s) requested by --staged, 1 file(s) scanned") {
		t.Errorf("a scanned path must be counted as requested:\n%s", out)
	}
}

// FIX B. The carve-out asked the WORKTREE what kind of entry this is, but
// --staged commits the INDEX. Stage a regular blob and then replace the path on
// disk with a directory or a symlink: git still names it, the walk cannot
// produce it, the worktree entry is not regular — and it was silently excused
// while the regular blob committed unchecked.
//
// The oracle under --staged is therefore the mode git records, not the mode on
// disk. `vcs.TrackedUnder` already reads `ls-files --stage` and drops 160000 for
// the neighbouring reason ("a gitlink is a pointer, not a file of this repo").
func TestCheckStagedBlobReplacedByADirectoryIsRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "x.txt"), "const x = \"WIDGET\"\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	mustRemove(t, filepath.Join(root, "src", "x.txt"))
	if err := os.Mkdir(filepath.Join(root, "src", "x.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the index holds a 100644 blob that no rule read\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "src/x.txt") {
		t.Errorf("stderr must name the path:\n%s", errOut)
	}
}

func TestCheckStagedBlobReplacedByASymlinkIsRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "x.txt"), "const x = \"WIDGET\"\n")
	mustWrite(t, filepath.Join(root, "src", "other.txt"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	mustRemove(t, filepath.Join(root, "src", "x.txt"))
	// A non-source name on purpose: a symlink NAMED *.go would fail the walk
	// under #54 and the row would pass for an unrelated reason.
	if err := os.Symlink("other.txt", filepath.Join(root, "src", "x.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — the index holds a 100644 blob, whatever the worktree now says\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "src/x.txt") {
		t.Errorf("stderr must name the path:\n%s", errOut)
	}
}

// The submodule case the worktree oracle could not see: a gitlink whose
// directory has never been initialised. `Lstat` says "not there" and the cure it
// earns — restore the file — cannot help, because there is no file to restore.
// Keyed on the index mode it is carved out whether or not the directory exists.
func TestCheckStagedUninitialisedGitlinkIsNotRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "a.txt"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	head := gitOut(t, root, "rev-parse", "HEAD")
	// No directory is created: this is a submodule that was never `init`ed.
	gitRun(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/sub")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — a gitlink is a pointer, not a file this repo scans\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "restore") {
		t.Errorf("there is no file to restore; that cure cannot help here:\n%s", errOut)
	}
}

// FIX C. The non-regular carve-out is asked BEFORE channel attribution, and that
// ordering silently narrowed a shipped guard. WalkWith consults ignore globs
// BEFORE #54's symlink refusal (pinned by scan/ignore_test.go's
// TestWalkIgnoringSourceSymlinkInsideIgnoredTreeDoesNotError), so inside a
// declared scan.ignore tree the walk SUCCEEDS on a source-extension symlink and
// the path does reach this accounting. Base refused it at exit 2 naming the
// glob; it is exit 0 now.
//
// Pinned deliberately rather than left to drift. The narrowing is defensible on
// the licensing invariant — the glob prunes that path in a whole-tree run too,
// so no coverage is lost — but it was unstated and untested, and the sentence
// that defended it ("that path never reaches this function in any mode") was
// simply false.
func TestCheckStagedSourceSymlinkInsideAnIgnoredTreeIsNotRefused(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: not ours\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: WIDGET}\n")
	// The symlink is the ONLY staged path inside the ignored tree. A regular
	// file beside it would be refused by the channel guard on its own account
	// and the row would pass without ever reaching the carve-out.
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, root)
	if err := os.Symlink("target.go", filepath.Join(root, "vendor", "alias.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gitRun(t, root, "add", "-A")

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the glob prunes this path in a whole-tree run too\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "vendor/alias.go") {
		t.Errorf("no coverage is lost, so there is nothing to refuse:\n%s", errOut)
	}
}

// ---------------------------------------------------------------------------
// --range: a pointer is a pointer in both modes (#158 review round 2).
// ---------------------------------------------------------------------------

// A gitlink is a POINTER, and formwork reads a submodule's contents in NO mode
// — not whole-tree, not --staged, not --range. So there is no coverage gap in
// either direction and nothing to refuse. --staged reaches that conclusion via
// the index mode; --range reached the opposite one via os.Lstat, and told the
// operator to `git restore` something that was never a file.
//
// The blast radius is ordinary CI: a checkout without --recurse-submodules
// leaves the gitlink's directory absent, so every range touching a submodule
// bump failed hard. This PR introduced that failure, so this PR fixes it.
func TestCheckRangeUninitialisedGitlinkIsNotRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "a.txt"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	head := gitOut(t, root, "rev-parse", "HEAD")
	// The bump commit. No directory is ever created — this is the shape a CI
	// checkout without --recurse-submodules leaves behind.
	gitRun(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/sub")
	gitRun(t, root, "commit", "-qm", "bump submodule")

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — a gitlink is a pointer in every mode\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "restore") {
		t.Errorf("there is no file to restore; that cure cannot help here:\n%s", errOut)
	}
}

func TestCheckRangeSymlinkIsNotRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "a.txt"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	if err := os.Symlink("a.txt", filepath.Join(root, "src", "alias.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "add symlink")

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "src/alias.txt") {
		t.Errorf("the walk produces symlinks for nobody, in any mode:\n%s", errOut)
	}
}

// The control that stops the row above widening into a hole. A path that was a
// REGULAR FILE at the range end and is simply missing on disk is the case
// --range exists to catch, and it must stay exit 2 with the cure that works.
func TestCheckRangeRegularFileMissingOnDiskIsStillRefused(t *testing.T) {
	root := carveoutRepo(t)
	mustWrite(t, filepath.Join(root, "src", "a.txt"), "clean\n")
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "src", "later.txt"), "const y = 1\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-qm", "second")
	mustRemove(t, filepath.Join(root, "src", "later.txt"))

	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "HEAD~1..HEAD")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — a regular file the range named was read by nothing\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "src/later.txt") {
		t.Errorf("stderr must name the path:\n%s", errOut)
	}
	if !strings.Contains(errOut, "restore them") {
		t.Errorf("a genuinely missing regular file must still get the cure that works:\n%s", errOut)
	}
}
