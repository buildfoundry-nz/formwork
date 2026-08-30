package vcs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// The parser's two hazards, in one range: a RENAME carries two path fields and
// must key on the DESTINATION (which is what RangePaths reports), and a gitlink
// must come back as 160000 rather than as whatever the worktree happens to hold
// — which for an uninitialised submodule is nothing at all. Getting either
// wrong makes the caller's pointer carve-out silently wrong in the direction
// that refuses a path nothing could ever have scanned.
func TestRangeModesReportsEndTreeModesAndKeysRenamesOnTheDestination(t *testing.T) {
	requireSymlinks(t)
	dir := initRepo(t)
	write(t, dir, "src/a.txt", "a\n")
	write(t, dir, "src/old.txt", "stable content, so the rename is detected\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "one")

	write(t, dir, "src/a.txt", "a2\n")
	run(t, dir, "mv", "src/old.txt", "src/new.txt")
	if err := os.Symlink("a.txt", filepath.Join(dir, "src", "link.txt")); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))
	// A gitlink whose directory is never created — the shape a CI checkout
	// without --recurse-submodules leaves behind.
	run(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/sub")
	run(t, dir, "add", "src/a.txt", "src/link.txt")
	run(t, dir, "commit", "-q", "-m", "two")

	modes, err := vcs.RangeModes(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"src/a.txt":    vcs.ModeBlob,
		"src/link.txt": vcs.ModeSymlink,
		"vendor/sub":   vcs.ModeGitlink,
		"src/new.txt":  vcs.ModeBlob, // the rename's DESTINATION
	} {
		if got := modes[path]; got != want {
			t.Errorf("modes[%q] = %q, want %q (all: %v)", path, got, want, modes)
		}
	}
	// The rename's SOURCE must not be keyed. RangePaths never reports it, so a
	// mode filed under that key can only mislead a caller that looks one up.
	if got, ok := modes["src/old.txt"]; ok {
		t.Errorf("modes[src/old.txt] = %q, want absent — the source is not a requested path", got)
	}
	// Every path the companion call reports must have a mode here: the caller
	// reads a missing mode as "not a pointer", so a gap between the two lists
	// is a refusal waiting to happen.
	paths, err := vcs.RangePaths(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if _, ok := modes[p]; !ok {
			t.Errorf("RangePaths reported %q but RangeModes has no mode for it", p)
		}
	}
}

// Every git failure is an error. The caller reads a missing mode as "not a
// pointer" and refuses, so answering "no modes" for a range that could not be
// read would refuse the whole changeset on a git problem.
func TestRangeModesFailsOnARangeGitCannotResolve(t *testing.T) {
	dir := initRepo(t)
	if _, err := vcs.RangeModes(dir, "nope..alsonope"); err == nil {
		t.Fatal("want an error for a range git cannot resolve")
	}
	if _, err := vcs.RangeModes(dir, "   "); err == nil {
		t.Fatal("want an error for an empty range")
	}
}

// This package has TWO path frames, deliberately, and StagedModes belongs to
// the strict one. The CHANGESET helpers — StagedPaths, RangePaths, TrackedPaths,
// RangeModes, Diff and this — answer in the REPOSITORY's frame, which holds only
// when root is the top level, because their output is intersected with scan
// paths that are repo-root-relative; git reports cwd-relative paths from a
// subdirectory and those match nothing. Each calls EnsureTopLevel.
//
// The ...Under family does NOT, and must not be "fixed" to. TrackedUnder,
// IgnoredUnder, CheckIgnored and Submodules are ROOT-relative by design;
// TrackedUnder's own doc carries the reason and the hedge it deserves — git's
// cwd-relative output is the frame lint's scan uses, and that "keeps the check
// usable if lint ever runs over a corpus that is a repo subdir" (#89). Adding
// EnsureTopLevel there would remove that property, not harden it.
//
// StagedModes was safe in production only because StagedPaths ran the check
// earlier in the same run; depending on call ORDER in another package is how a
// guard goes quietly inert. A subdirectory must be refused here, not answered.
func TestStagedModesRefusesASubdirectory(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "src/a.txt", "a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "one")

	if _, err := vcs.StagedModes(dir, []string{"src/a.txt"}); err != nil {
		t.Fatalf("the top level must be accepted: %v", err)
	}
	sub := filepath.Join(dir, "src")
	if _, err := vcs.StagedModes(sub, []string{"a.txt"}); err == nil {
		t.Fatal("want an error: a subdirectory answers in the wrong path frame")
	}
	// Empty input still returns early, so the guard cannot be reached by a
	// caller with nothing to ask about — which is also the common case.
	if got, err := vcs.StagedModes(sub, nil); err != nil || len(got) != 0 {
		t.Errorf("empty input must stay a cheap no-op, got %v / %v", got, err)
	}
}
