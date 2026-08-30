package vcs_test

import (
	"reflect"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// #175's THIRD INDEX-READING ANSWER. IgnoredUnder and CheckIgnored were the two
// the fix covered; TrackedUnder is the same shape — `ls-files --stage` reads the
// index, and GIT_INDEX_FILE moves it. Its consumer is the `scan-ignore-tracked`
// check (#90, internal/meta/lint.go), whose whole premise is "pruning is sound
// only while pruned paths stay uncommitted": under an index that tracks less
// than the repository's, a committed file hidden by a `scan.ignore` glob is
// absent from the tracked set and lint reports clean. Same variable, same
// package, same silent-pass direction.
//
// movedIndexRepo (ignored_test.go) commits .gitignore and a force-added
// build/keep.go and then points GIT_INDEX_FILE at a well-formed EMPTY index, so
// the ambient answer is "nothing is tracked".
func TestTrackedUnderStillTracksACommittedFileUnderAMovedIndex(t *testing.T) {
	dir := movedIndexRepo(t)
	got, err := vcs.TrackedUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "build/keep.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder under a moved index = %v, want %v — a committed file that is not in the tracked set makes scan-ignore-tracked report clean over it", got, want)
	}
}

// THE OTHER HALF OF THE UNION, and the reason this is a union rather than #175's
// intersection: the fail-closed direction for "what does the repository track"
// is that a path EITHER index calls tracked is tracked. A path `git rm --cached`
// dropped from the real index is back in the temporary index a partial commit
// builds from HEAD, so the repository's index alone would call it untracked
// while the commit carries it — and a `scan.ignore` glob over it would then pass
// silently for the run that is actually committing it.
//
// REGRESSION GUARD, not a red-green: before this change TrackedUnder returned
// the ambient answer alone, which already satisfies it. Its proof is the
// mutation — making the union return repoSide alone fails this test and no
// other.
func TestTrackedUnderKeepsAPathOnlyTheIndexBeingCommittedHolds(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.txt", "one\n")
	write(t, dir, "scratch.txt", "s")
	run(t, dir, "add", "a.txt", "scratch.txt")
	run(t, dir, "commit", "-q", "-m", "base")
	run(t, dir, "rm", "-q", "--cached", "scratch.txt") // out of the real index, still in HEAD

	// The temporary index `git commit -- a.txt` builds: HEAD plus the named path,
	// so scratch.txt is back.
	setIndex(t, dir, func(idx string) {
		gitWithIndex(t, dir, idx, "read-tree", "HEAD")
		gitWithIndex(t, dir, idx, "add", "a.txt")
	})

	got, err := vcs.TrackedUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "scratch.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TrackedUnder = %v, want %v — scratch.txt is in the index being committed", got, want)
	}
}
