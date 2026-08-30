package vcs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// resolved is EvalSymlinks with a t.Fatal on failure. Every path assertion in
// this file needs it: git reports worktree paths and an out-of-tree hooks
// directory as the kernel resolves them, while t.TempDir() hands back the
// spelling it was given — on macOS those differ by a leading /private.
func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return r
}

// commitRepo is initRepo plus one commit, which `git worktree add` requires.
func commitRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	write(t, dir, "a.txt", "hi\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func findWorktree(t *testing.T, wts []vcs.Worktree, path string) vcs.Worktree {
	t.Helper()
	want := resolved(t, path)
	for _, w := range wts {
		if w.Path == want {
			return w
		}
	}
	var got []string
	for _, w := range wts {
		got = append(got, w.Path)
	}
	t.Fatalf("no worktree at %q; got %q", want, got)
	return vcs.Worktree{}
}

func TestHooksPathUnsetIsGitsDefaultHooksDir(t *testing.T) {
	dir := initRepo(t)
	got, err := vcs.HooksPath(dir)
	if err != nil {
		t.Fatalf("HooksPath: %v", err)
	}
	if want := filepath.Join(dir, ".git", "hooks"); got != want {
		t.Fatalf("HooksPath = %q, want %q", got, want)
	}
}

func TestHooksPathResolvesRelativeValueAgainstRoot(t *testing.T) {
	dir := initRepo(t)
	run(t, dir, "config", "core.hooksPath", ".formwork/hooks")
	got, err := vcs.HooksPath(dir)
	if err != nil {
		t.Fatalf("HooksPath: %v", err)
	}
	if want := filepath.Join(dir, ".formwork", "hooks"); got != want {
		t.Fatalf("HooksPath = %q, want %q", got, want)
	}
}

// An absolute core.hooksPath must come back untouched. Joining it against root
// concatenates two absolute paths into a directory that cannot exist, so the
// caller is sent somewhere that was never real.
//
// Mutation: delete the filepath.IsAbs branch (join unconditionally). This test
// fails — the doubled path is not the configured one.
func TestHooksPathReturnsAbsoluteValueUnjoined(t *testing.T) {
	dir := initRepo(t)
	abs := filepath.Join(dir, "elsewhere", "hooks")
	run(t, dir, "config", "core.hooksPath", abs)
	got, err := vcs.HooksPath(dir)
	if err != nil {
		t.Fatalf("HooksPath: %v", err)
	}
	if got != abs {
		t.Fatalf("HooksPath = %q, want %q", got, abs)
	}
}

// git echoes core.hooksPath as spelled, trailing separator and all. A caller
// comparing or joining wants one spelling, not two — and BOTH arms need
// normalising, since the relative arm gets it free from filepath.Join while the
// absolute arm returns git's string directly.
//
// Mutation: drop filepath.Clean from the absolute arm. The absolute subtest
// fails.
func TestHooksPathNormalizesTrailingSlash(t *testing.T) {
	dir := initRepo(t)
	want := filepath.Join(dir, ".formwork", "hooks")
	for name, val := range map[string]string{
		"relative": ".formwork/hooks/",
		"absolute": want + "/",
	} {
		t.Run(name, func(t *testing.T) {
			run(t, dir, "config", "core.hooksPath", val)
			got, err := vcs.HooksPath(dir)
			if err != nil {
				t.Fatalf("HooksPath: %v", err)
			}
			if got != want {
				t.Fatalf("HooksPath = %q, want %q", got, want)
			}
		})
	}
}

// Inside a linked worktree with core.hooksPath unset, git answers with the MAIN
// repository's absolute .git/hooks. This is the state that makes the IsAbs
// branch mandatory rather than defensive: the answer is absolute even though
// nobody configured an absolute path.
//
// Mutation: delete the filepath.IsAbs branch. This test fails — the joined path
// does not exist, so EvalSymlinks errors.
func TestHooksPathInLinkedWorktreeIsAbsoluteAndUnjoined(t *testing.T) {
	main := commitRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, main, "worktree", "add", "-q", linked, "-b", "wt")

	got, err := vcs.HooksPath(linked)
	if err != nil {
		t.Fatalf("HooksPath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("HooksPath = %q, want an absolute path", got)
	}
	want := resolved(t, filepath.Join(main, ".git", "hooks"))
	if resolved(t, got) != want {
		t.Fatalf("HooksPath = %q (resolves to %q), want %q", got, resolved(t, got), want)
	}
}

// Both doc comments promise an ABSOLUTE path, and filepath.Join does not make
// one: the answer is relative whenever root is. The CLI's default root is ".",
// so this is the commonest call there is, and its consumer compares the result
// against paths git reports absolutely — a comparison a relative spelling loses
// silently rather than loudly.
//
// Mutation: drop the filepath.Abs from either function — its row fails.
func TestHooksPathAndCommonDirAreAbsoluteFromARelativeRoot(t *testing.T) {
	dir := initRepo(t)
	t.Chdir(dir)

	for name, fn := range map[string]func(string) (string, error){
		"HooksPath": vcs.HooksPath,
		"CommonDir": vcs.CommonDir,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := fn(".")
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("%s(\".\") = %q, which is not absolute", name, got)
			}
			// Absolute AND still the same directory: an absolutisation that
			// pointed somewhere else would satisfy the check above.
			if want := resolved(t, filepath.Join(dir, ".git")); resolved(t, got) != want && resolved(t, filepath.Dir(got)) != want {
				t.Fatalf("%s(\".\") = %q, which does not resolve under %q", name, got, want)
			}
		})
	}
}

func TestHooksPathNotARepoErrors(t *testing.T) {
	if _, err := vcs.HooksPath(t.TempDir()); err == nil {
		t.Fatal("HooksPath outside a repository: want an error, got nil")
	}
}

func TestWorktreesReportsMainAndLinkedAsPlainSites(t *testing.T) {
	main := commitRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, main, "worktree", "add", "-q", linked, "-b", "wt")

	wts, err := vcs.Worktrees(main)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if len(wts) != 2 {
		t.Fatalf("Worktrees returned %d entries, want 2: %+v", len(wts), wts)
	}
	for _, p := range []string{main, linked} {
		w := findWorktree(t, wts, p)
		if w.Bare || w.Prunable || w.Locked {
			t.Fatalf("worktree %q: want no state flags, got %+v", p, w)
		}
	}
}

// A worktree whose directory has been deleted is still LISTED by git, marked
// prunable. Verify has to tell that apart from a worktree that exists but lacks
// shims, so the flag has to survive parsing.
func TestWorktreesReportsPrunable(t *testing.T) {
	main := commitRepo(t)
	gone := filepath.Join(t.TempDir(), "gone")
	run(t, main, "worktree", "add", "-q", gone, "-b", "wt")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	wts, err := vcs.Worktrees(main)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	// The directory is gone, so its path cannot be EvalSymlinks'd; match on the
	// one entry that is not the main worktree.
	var found bool
	for _, w := range wts {
		if w.Path == resolved(t, main) {
			continue
		}
		found = true
		if !w.Prunable {
			t.Fatalf("worktree %q: want Prunable, got %+v", w.Path, w)
		}
	}
	if !found {
		t.Fatalf("a deleted worktree was not listed at all: %+v", wts)
	}
}

func TestWorktreesReportsLocked(t *testing.T) {
	main := commitRepo(t)
	locked := filepath.Join(t.TempDir(), "locked")
	run(t, main, "worktree", "add", "-q", locked, "-b", "wt")
	run(t, main, "worktree", "lock", locked)

	wts, err := vcs.Worktrees(main)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if w := findWorktree(t, wts, locked); !w.Locked {
		t.Fatalf("worktree %q: want Locked, got %+v", locked, w)
	}
}

func TestWorktreesReportsBare(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "--bare", "b.git")
	bare := filepath.Join(dir, "b.git")

	wts, err := vcs.Worktrees(bare)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if len(wts) != 1 {
		t.Fatalf("Worktrees returned %d entries, want 1: %+v", len(wts), wts)
	}
	if w := findWorktree(t, wts, bare); !w.Bare {
		t.Fatalf("worktree %q: want Bare, got %+v", bare, w)
	}
}

// THE REASON FOR -z. `git worktree list --porcelain` emits the path raw, so a
// worktree at a path containing a newline spills across lines. A line parser
// does not error on that — it reads the first line as the whole path and treats
// the remainder as an unrecognised attribute, silently reporting a DIFFERENT,
// possibly real, directory. Verify would then certify a hook site nobody has.
//
// Mutation: drop -z from the git arguments and split records on "\n". This test
// fails: the reported path is truncated at the newline, and no error is raised.
func TestWorktreesSurvivesNewlineInWorktreePath(t *testing.T) {
	main := commitRepo(t)
	nl := filepath.Join(t.TempDir(), "nl\nwt")
	run(t, main, "worktree", "add", "-q", nl, "-b", "wt")

	wts, err := vcs.Worktrees(main)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	w := findWorktree(t, wts, nl)
	if !strings.Contains(w.Path, "\n") {
		t.Fatalf("worktree path %q lost its newline", w.Path)
	}
	// The truncated prefix must not be what got reported.
	if w.Path == resolved(t, filepath.Dir(nl))+string(filepath.Separator)+"nl" {
		t.Fatalf("worktree path %q was truncated at the newline", w.Path)
	}
}

// Fail closed: a record that does not parse is an error, never a dropped entry.
// Skipping it is the fail-open — the skipped worktree may be one that exists on
// a branch without the shims, and Verify would report the repo wired.
//
// Mutation: replace the error return with `continue`. This test fails.
func TestWorktreesMalformedRecordIsAnError(t *testing.T) {
	for name, out := range map[string]string{
		"record does not start with worktree":  "HEAD abc123\x00branch refs/heads/main\x00\x00",
		"worktree attribute carries no path":   "worktree\x00\x00",
		"worktree attribute has an empty path": "worktree \x00\x00",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := vcs.ParseWorktreesForTest(out)
			if err == nil {
				t.Fatalf("want an error, got %+v", got)
			}
		})
	}
}

func TestParseWorktreesAcceptsGitsRealOutput(t *testing.T) {
	// Byte-for-byte the shape measured from git 2.50.1, including the bare
	// record's missing HEAD/branch attributes and the trailing empty field the
	// final NUL leaves behind.
	out := "worktree /r/main\x00HEAD abc\x00branch refs/heads/main\x00\x00" +
		"worktree /r/gone\x00HEAD abc\x00branch refs/heads/g\x00prunable gitdir file points to non-existent location\x00\x00" +
		"worktree /r/lockd\x00HEAD abc\x00branch refs/heads/l\x00locked because\x00\x00" +
		"worktree /r/b.git\x00bare\x00\x00"
	got, err := vcs.ParseWorktreesForTest(out)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	want := []vcs.Worktree{
		{Path: "/r/main"},
		{Path: "/r/gone", Prunable: true, PrunableReason: "gitdir file points to non-existent location"},
		{Path: "/r/lockd", Locked: true},
		{Path: "/r/b.git", Bare: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d worktrees, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("worktree %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// git's reason is the difference between two states with opposite cures: a
// worktree whose directory is really gone, and one whose gitdir file no longer
// resolves because the worktree was moved and is still working. Discarding it
// forces the caller to invent a reason, which is what verify used to do.
//
// Mutation: drop the reason and keep only the flag — the first row fails.
func TestParseWorktreesCarriesGitsPrunableReason(t *testing.T) {
	got, err := vcs.ParseWorktreesForTest(
		"worktree /r/a\x00prunable gitdir file points to non-existent location\x00\x00" +
			"worktree /r/b\x00prunable\x00\x00") // the attribute may carry no reason
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	want := []vcs.Worktree{
		{Path: "/r/a", Prunable: true, PrunableReason: "gitdir file points to non-existent location"},
		{Path: "/r/b", Prunable: true},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("worktree %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWorktreesNotARepoErrors(t *testing.T) {
	if _, err := vcs.Worktrees(t.TempDir()); err == nil {
		t.Fatal("Worktrees outside a repository: want an error, got nil")
	}
}

// CommonDir's whole reason to exist is the linked-worktree case: `--git-dir`
// there names the per-worktree directory, which has no hooks/ at all, while the
// hooks git would fall back to live under the common directory. A caller that
// asked the wrong question concludes the repository has no hooks of its own —
// wrong in the direction of overriding someone else's gate.
func TestCommonDirInLinkedWorktreeIsTheMainRepositorysGitDir(t *testing.T) {
	main := commitRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, main, "worktree", "add", "-q", linked, "-b", "wt")

	got, err := vcs.CommonDir(linked)
	if err != nil {
		t.Fatalf("CommonDir: %v", err)
	}
	if want := resolved(t, filepath.Join(main, ".git")); resolved(t, got) != want {
		t.Fatalf("CommonDir = %q (resolves to %q), want %q", got, resolved(t, got), want)
	}
}

// From a subdirectory git answers `../.git`, relative to the directory it ran
// in. filepath.Join(root, "../.git") is the only reading that lands anywhere
// real.
func TestCommonDirResolvesRelativeAnswerAgainstRoot(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := vcs.CommonDir(sub)
	if err != nil {
		t.Fatalf("CommonDir: %v", err)
	}
	if want := resolved(t, filepath.Join(dir, ".git")); resolved(t, got) != want {
		t.Fatalf("CommonDir = %q (resolves to %q), want %q", got, resolved(t, got), want)
	}
}

func TestCommonDirNotARepoErrors(t *testing.T) {
	if _, err := vcs.CommonDir(t.TempDir()); err == nil {
		t.Fatal("CommonDir outside a repository: want an error, got nil")
	}
}
