package vcs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// paths reduces records to their paths, for assertions that only care about
// which paths were reported.
func paths(recs []vcs.IgnoredPath) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Path
	}
	return out
}

func TestIgnoredUnderReportsIgnoredFilesAndCollapsedDirs(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "build/\nscratch.txt\n")
	write(t, dir, "build/a.txt", "x")
	write(t, dir, "build/b.txt", "y")
	write(t, dir, "scratch.txt", "s")
	write(t, dir, "keep.go", "package main")
	write(t, dir, "untracked.go", "package main")
	run(t, dir, "add", ".gitignore", "keep.go")

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	// build/ collapses to one dir record — the walk must be able to prune the
	// subtree without descending. untracked.go is untracked but NOT ignored, so
	// it must stay out of the prune set entirely (that file is exactly the
	// coverage #80 protects).
	if want := []string{"build", "scratch.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("paths = %v, want %v", paths(got), want)
	}
	if !got[0].Dir {
		t.Errorf("build: Dir = false, want true (a collapsed subtree)")
	}
	if got[1].Dir {
		t.Errorf("scratch.txt: Dir = true, want false")
	}
}

// A tracked path is never ignored, whatever .gitignore says — git enforces
// this itself (check-ignore reports a tracked path as not-ignored unless
// --no-index). This test is the load-bearing one: it is the property that
// makes pruning the ignored set narrower than pruning the untracked set, and
// so the reason this mechanism is not the fail-open change #80 rejects.
func TestIgnoredUnderNeverReportsATrackedPath(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "build/\n")
	write(t, dir, "build/a.txt", "x")
	write(t, dir, "build/keep.go", "package main")
	run(t, dir, "add", ".gitignore")
	run(t, dir, "add", "-f", "build/keep.go")

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.Path == "build/keep.go" || (r.Dir && r.Path == "build") {
			t.Fatalf("force-added tracked file is hidden by %+v; every record: %v", r, paths(got))
		}
	}
	// The ignored sibling is still reported — the tracked file suppresses the
	// subtree collapse, not the prune of what is genuinely ignored.
	if want := []string{"build/a.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("paths = %v, want %v", paths(got), want)
	}
}

// `ls-files --others --ignored --directory` also collapses a directory that is
// merely UNTRACKED-throughout, not ignored — here a/ and a/b/, neither of which
// any .gitignore line matches. Pruning those would skip paths git does not
// ignore, so they must be dropped and only the genuinely ignored descendant
// kept. Without the check-ignore confirmation pass this returns a/ and the
// walk never reaches a/b/keep-me.go.
func TestIgnoredUnderDropsCollapsedButUnignoredDirs(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "ignored/\n")
	write(t, dir, "a/b/ignored/deep.txt", "x")
	write(t, dir, "root.go", "package main")
	run(t, dir, "add", ".gitignore", "root.go")

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/b/ignored"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("paths = %v, want %v — a/ and a/b/ are untracked, not ignored", paths(got), want)
	}
}

func TestIgnoredUnderCarriesTheResponsibleRule(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "# a comment\nscratch.txt\n")
	write(t, dir, "scratch.txt", "s")
	run(t, dir, "add", ".gitignore")

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1: %v", len(got), paths(got))
	}
	r := got[0]
	if r.Source != ".gitignore" || r.Line != 2 || r.Pattern != "scratch.txt" {
		t.Errorf("provenance = %s:%d:%s, want .gitignore:2:scratch.txt", r.Source, r.Line, r.Pattern)
	}
}

// check-ignore exits 1 when nothing is ignored. That is an answer, not a
// failure, and conflating the two would make an ordinary clean repo look like
// an engine error.
func TestIgnoredUnderNothingIgnoredIsEmptyNotAnError(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "keep.go", "package main")
	run(t, dir, "add", "keep.go")

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatalf("clean repo reported an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want no records", paths(got))
	}
}

// Fail-closed: "could not determine" must never arrive as "nothing ignored".
// The caller decides what to do with the error; it must get one.
func TestIgnoredUnderNotARepoErrors(t *testing.T) {
	if _, err := vcs.IgnoredUnder(t.TempDir()); err == nil {
		t.Fatal("IgnoredUnder in a non-repo returned nil error")
	}
}

// #96's lesson, applied here: with default core.quotepath git C-quotes any
// path holding a non-ASCII byte, and a quoted spelling can never match a scan
// path — the prune would silently miss, or worse, name a path that does not
// exist. Both git calls in this seam must be -z.
func TestIgnoredUnderSurvivesQuoteworthyPaths(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "café.txt\n")
	write(t, dir, "café.txt", "x")
	run(t, dir, "add", ".gitignore")

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"café.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("paths = %q, want %q", paths(got), want)
	}
}

// CheckIgnored answers for a BATCH of paths, existing or not — the ghost half
// of the #122 guidance contract. IgnoredUnder snapshots only paths that exist,
// so a consumer asking about files about to be written needs pattern
// evaluation, which check-ignore performs for any pathname; one --stdin call
// serves the whole batch.
func TestCheckIgnoredEvaluatesGhostPaths(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "build/\n*.bin\n")
	run(t, dir, "add", ".gitignore")

	recs, err := vcs.CheckIgnored(dir, []string{"build/out.go", "gen/new.bin", "src/free.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("recs = %+v, want the two pattern-matched ghosts only", recs)
	}
	if recs[0].Path != "build/out.go" || recs[0].Line != 1 || recs[0].Pattern != "build/" {
		t.Fatalf("ghost under dir pattern: %+v, want .gitignore:1:build/", recs[0])
	}
	if recs[1].Path != "gen/new.bin" || recs[1].Line != 2 || recs[1].Pattern != "*.bin" {
		t.Fatalf("ghost under leaf pattern: %+v, want .gitignore:2:*.bin", recs[1])
	}
}

// A record whose deciding pattern is a NEGATION is git saying the path is
// explicitly NOT ignored — check-ignore -v still emits it and exits 0, and
// treating it as an ignore verdict would tell a guidance consumer "not
// scanned" about a path the walk will scan and enforce on (#125 review
// finding 1, reproduced end-to-end before this test pinned it).
func TestCheckIgnoredNegatedPatternIsNotIgnored(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "*.log\n!important.log\n")
	run(t, dir, "add", ".gitignore")

	recs, err := vcs.CheckIgnored(dir, []string{"important.log", "debug.log"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Path != "debug.log" || recs[0].Pattern != "*.log" {
		t.Fatalf("recs = %+v, want only debug.log via *.log — a negation record is a not-ignored verdict", recs)
	}
}

// Not-ignored is an absent record, never an error — and a TRACKED path is
// never ignored whatever .gitignore says (no --no-index; git's own carve-out,
// same guarantee IgnoredUnder leans on).
func TestCheckIgnoredNotIgnoredAndTrackedAreAbsent(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "vendor/\n")
	write(t, dir, "vendor/kept.go", "package v")
	run(t, dir, "add", ".gitignore")
	run(t, dir, "add", "-f", "vendor/kept.go")

	recs, err := vcs.CheckIgnored(dir, []string{"src/free.go", "vendor/kept.go"})
	if err != nil || len(recs) != 0 {
		t.Fatalf("recs=%+v err=%v, want empty, nil (unmatched ghost + tracked carve-out)", recs, err)
	}
}

// Fail-closed: an unanswerable question is an error, never "not ignored".
func TestCheckIgnoredNotARepoErrors(t *testing.T) {
	if _, err := vcs.CheckIgnored(t.TempDir(), []string{"x.go"}); err == nil {
		t.Fatal("CheckIgnored in a non-repo returned nil error")
	}
}

// Submodules feeds the guidance layer's candidate prefilter: check-ignore
// FATALS (exit 128) on any pathspec under a registered submodule, and the
// walk is submodule-oblivious — so those candidates must be excluded before
// the batch, not surfaced as an unanswerable channel.
func TestSubmodulesListsGitlinks(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "seed.txt", "s")
	run(t, dir, "add", "seed.txt")
	run(t, dir, "commit", "-q", "-m", "seed")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(string(out))+",subdir/mod")

	subs, err := vcs.Submodules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(subs, []string{"subdir/mod"}) {
		t.Fatalf("Submodules = %v, want [subdir/mod]", subs)
	}
}

func TestSubmodulesEmptyAndNotARepo(t *testing.T) {
	dir := initRepo(t)
	subs, err := vcs.Submodules(dir)
	if err != nil || len(subs) != 0 {
		t.Fatalf("clean repo: subs=%v err=%v, want empty, nil", subs, err)
	}
	if _, err := vcs.Submodules(t.TempDir()); err == nil {
		t.Fatal("Submodules in a non-repo returned nil error")
	}
}

// #175: THE TRACKED CARVE-OUT IS ONLY AS GOOD AS THE INDEX GIT READ.
//
// `check-ignore` refuses to call a TRACKED path ignored, and that refusal is
// what makes pruning the ignored set narrower than pruning the untracked set
// (#80, #90). It is an answer about the index git resolved, and GIT_INDEX_FILE
// moves that index. Measured on git 2.50.1 with a well-formed EMPTY index:
// nothing is tracked, `ls-files --others --ignored --directory` collapses the
// whole build/ subtree, check-ignore confirms it, and a COMMITTED file inside it
// is pruned — `check` exits 0 over a violation it never read. A TRUNCATED index
// is loud (git errors, nothing is pruned); a well-formed wrong one is silent,
// which is what makes this worth a guard.
//
// The answer must be the one the repository's own index gives: build/keep.go
// scanned, build/a.txt still pruned. "Prune nothing whenever GIT_INDEX_FILE is
// set" would also pass the first half and is why the second is asserted.
func TestIgnoredUnderNeverPrunesUnderAMovedIndex(t *testing.T) {
	dir := movedIndexRepo(t)
	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build/a.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("IgnoredUnder under a moved index = %v, want %v — a committed file inside build/ is pruned, so check never reads it", paths(got), want)
	}
}

// The same defect through the guidance seam (#122). CheckIgnored answers "is
// this path scanned" for `formwork rules-for`, so a moved index makes it tell an
// operator a tracked, enforced-upon file is not scanned.
func TestCheckIgnoredNeverCallsATrackedPathIgnoredUnderAMovedIndex(t *testing.T) {
	dir := movedIndexRepo(t)
	got, err := vcs.CheckIgnored(dir, []string{"build/keep.go", "build/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build/a.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("CheckIgnored under a moved index = %v, want %v", paths(got), want)
	}
}

// GIT'S OWN HOOK ENVIRONMENT REACHES THIS, with no hostile actor: during a
// partial commit (`git commit -- a.txt`) git builds a temporary index from HEAD
// plus the named paths and points GIT_INDEX_FILE at it before running
// pre-commit. A file force-added but not yet COMMITTED is in the repository's
// real index and not in that temporary one — so under the temporary index git
// calls it untracked, the whole ignored directory collapses, and formwork's
// pre-commit lane prunes a path the repository tracks. Measured on git 2.50.1
// (the temp index there was `.git/next-index-<pid>.lock`).
func TestIgnoredUnderNeverPrunesAForceAddedFileDuringAPartialCommit(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "build/\n")
	write(t, dir, "a.txt", "one\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "build/x.go", "package main")
	write(t, dir, "build/a.txt", "x")
	run(t, dir, "add", "-f", "build/x.go") // real index only, not HEAD
	write(t, dir, "a.txt", "one changed\n")
	run(t, dir, "add", "a.txt")

	// The temporary index `git commit -- a.txt` builds: HEAD plus the named path.
	setIndex(t, dir, func(idx string) {
		gitWithIndex(t, dir, idx, "read-tree", "HEAD")
		gitWithIndex(t, dir, idx, "add", "a.txt")
	})

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build/a.txt"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("IgnoredUnder during a partial commit = %v, want %v — build/x.go is in the repository's index and must not be pruned", paths(got), want)
	}
}

// movedIndexRepo commits a force-added file inside an ignored directory, leaves
// an ignored sibling untracked, and points GIT_INDEX_FILE at a well-formed EMPTY
// index — the #175 reproduction, in which git reports nothing as tracked.
func movedIndexRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	write(t, dir, ".gitignore", "build/\n")
	write(t, dir, "build/keep.go", "package main")
	write(t, dir, "build/a.txt", "x")
	run(t, dir, "add", ".gitignore")
	run(t, dir, "add", "-f", "build/keep.go")
	run(t, dir, "commit", "-q", "-m", "base")
	setIndex(t, dir, func(idx string) { gitWithIndex(t, dir, idx, "read-tree", "--empty") })
	return dir
}

// setIndex builds an index file with build and exports GIT_INDEX_FILE at it for
// the rest of the test.
func setIndex(t *testing.T, dir string, build func(idx string)) {
	t.Helper()
	idx := filepath.Join(t.TempDir(), "next-index")
	build(idx)
	t.Setenv("GIT_INDEX_FILE", idx)
}

func gitWithIndex(t *testing.T, dir, idx string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+idx)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v with GIT_INDEX_FILE=%s: %v\n%s", args, idx, err, out)
	}
}

// THE OTHER DIRECTION OF THE #175 INTERSECTION: the ambient index gets a veto
// too, so "answer from the repository's own index instead" would be a fix in one
// direction and a new hole in the other.
//
// A path that HEAD carries and `git rm --cached` has removed from the real index
// is back in the temporary index a partial commit builds from HEAD — it is going
// into the commit. The repository's index calls it untracked and therefore
// ignored; pruning on that answer would hide a file the changeset carries. It
// must stay scanned, while a path both indexes call ignored is still pruned.
//
// This one is a REGRESSION GUARD rather than a red-green: before the #175 fix
// IgnoredUnder returned the ambient answer alone, which already satisfies it.
// Its proof is the mutation — making coveredBy answer true unconditionally, so
// the repository's answer is taken whole, fails this test and no other.
func TestIgnoredUnderKeepsAPathTheCommitCarriesEvenWhenTheRealIndexDropped(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, ".gitignore", "build/\nscratch.txt\n")
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "scratch.txt", "s")
	write(t, dir, "build/a.txt", "x")
	run(t, dir, "add", ".gitignore", "a.txt")
	run(t, dir, "add", "-f", "scratch.txt")
	run(t, dir, "commit", "-q", "-m", "base")
	run(t, dir, "rm", "-q", "--cached", "scratch.txt") // out of the index, still in HEAD

	setIndex(t, dir, func(idx string) {
		gitWithIndex(t, dir, idx, "read-tree", "HEAD")
		gitWithIndex(t, dir, idx, "add", "a.txt")
	})

	got, err := vcs.IgnoredUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build"}; !reflect.DeepEqual(paths(got), want) {
		t.Fatalf("IgnoredUnder = %v, want %v — scratch.txt is in the index being committed, so pruning it hides a file the commit carries", paths(got), want)
	}
}
