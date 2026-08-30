// gitignore_check_test.go — end-to-end proof for scan.gitignore (#100): the
// finding a gitignored file used to produce, and the findings it must still
// produce for everything git has not refused.
package cli_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitignoreRepo builds a repo whose one rule bans "banana", declaring
// scan.gitignore unless declare is false. Returns the root.
func gitignoreRepo(t *testing.T, declare bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	cfg := "version: 1\n"
	if declare {
		cfg += "scan:\n  gitignore:\n    reason: git already refuses these\n"
	}
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), cfg)
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: banana}\n")
	return root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// In the validating port, harness ephemera, gitignored and rewritten every
// turn, failed a governance rule on content that can never reach a commit.
// The same shape as that port's Flutter build artefacts.
func TestCheckDoesNotFireOnGitignoredContent(t *testing.T) {
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	mustWrite(t, filepath.Join(root, ".gitignore"), ".turn-state.txt\nbuild/\n")
	mustWrite(t, filepath.Join(root, ".turn-state.txt"), "banana\n")
	mustWrite(t, filepath.Join(root, "build", "out.js"), "banana\n")
	mustWrite(t, filepath.Join(root, "clean.txt"), "apples only\n")
	git(t, root, "add", ".gitignore", "clean.txt", ".formwork")

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, ".turn-state.txt") || strings.Contains(out, "out.js") {
		t.Fatalf("gitignored path produced a finding:\n%s", out)
	}
}

// The control. Without this the test above proves only that the rule stopped
// working: identical content at a path git has NOT refused must still fail.
func TestCheckStillFiresOnTrackableContent(t *testing.T) {
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	mustWrite(t, filepath.Join(root, ".gitignore"), ".turn-state.txt\n")
	mustWrite(t, filepath.Join(root, "src", "real.txt"), "banana\n")
	git(t, root, "add", ".gitignore", "src/real.txt", ".formwork")

	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "src/real.txt:1") {
		t.Fatalf("tracked violation not reported:\n%s", out)
	}
}

// The property that makes this narrower than "skip what is untracked" (#80):
// a brand-new file the developer has not staged yet is still gated. A
// pre-commit hook legitimately wants to see it.
func TestCheckStillFiresOnUntrackedButNotIgnoredContent(t *testing.T) {
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	mustWrite(t, filepath.Join(root, ".gitignore"), "build/\n")
	mustWrite(t, filepath.Join(root, "new-but-unstaged.txt"), "banana\n")
	git(t, root, "add", ".gitignore", ".formwork")

	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — an untracked, non-ignored file must stay in scope\n%s", code, out)
	}
	if !strings.Contains(out, "new-but-unstaged.txt:1") {
		t.Fatalf("untracked non-ignored violation not reported:\n%s", out)
	}
}

// The fail-open boundary, enforced by git itself: `git add -f` puts a file
// under an ignored directory into the index, and check-ignore then refuses to
// call it ignored. It must be scanned like any other tracked file.
func TestCheckStillFiresOnForceAddedFileUnderIgnoredDir(t *testing.T) {
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	mustWrite(t, filepath.Join(root, ".gitignore"), "build/\n")
	mustWrite(t, filepath.Join(root, "build", "noise.txt"), "banana\n")
	mustWrite(t, filepath.Join(root, "build", "tracked.txt"), "banana\n")
	git(t, root, "add", ".gitignore", ".formwork")
	git(t, root, "add", "-f", "build/tracked.txt")

	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — a force-added tracked file must stay in scope\n%s", code, out)
	}
	if !strings.Contains(out, "build/tracked.txt:1") {
		t.Fatalf("force-added tracked violation not reported:\n%s", out)
	}
	if strings.Contains(out, "build/noise.txt") {
		t.Fatalf("the genuinely ignored sibling still fired:\n%s", out)
	}
}

// Undeclared is unchanged: the same tree, without the key, still reports every
// gitignored path. This is what makes the feature opt-in rather than a silent
// coverage cut for every existing consumer.
func TestCheckWithoutTheKeyStillScansGitignoredContent(t *testing.T) {
	root := gitignoreRepo(t, false)
	git(t, root, "init", "-q")
	mustWrite(t, filepath.Join(root, ".gitignore"), ".turn-state.txt\n")
	mustWrite(t, filepath.Join(root, ".turn-state.txt"), "banana\n")
	git(t, root, "add", ".gitignore", ".formwork")

	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — without the key nothing is pruned\n%s", code, out)
	}
	if !strings.Contains(out, ".turn-state.txt:1") {
		t.Fatalf("undeclared repo stopped scanning a gitignored path:\n%s", out)
	}
}

// Declared, but the tree is not a repository — an exported tarball, a corpus
// checked out standalone. Pruning nothing is the fail-closed direction, so the
// run proceeds and reports MORE than declared; it must say so rather than
// look like an ordinary clean run.
func TestCheckSaysSoWhenGitCannotAnswer(t *testing.T) {
	root := gitignoreRepo(t, true) // no git init: deliberately not a repo
	mustWrite(t, filepath.Join(root, "noise.txt"), "banana\n")

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — nothing pruned means everything scanned\n%s", code, out)
	}
	if !strings.Contains(errOut, "could not determine") || !strings.Contains(errOut, "nothing pruned") {
		t.Fatalf("stderr must name the unanswered question:\n%s", errOut)
	}
}
