// The scrub policy, asserted across the package boundary (#307).
//
// WHY THIS FILE EXISTS SEPARATELY FROM historyvars_test.go. That file asserts
// what internal/vcs's OWN git calls answer, and it is complete for that. What it
// cannot see is a caller outside this package, because until #307 there was no
// way for one to reach the policy at all — so internal/buildloop wrote its own,
// removing three of the nine under the comment "matching internal/vcs's policy",
// and `seat-checkout cut <eng> <sha-only-in-oth> <dest>` checked out oth's tree
// at exit 0 under an ambient GIT_ALTERNATE_OBJECT_DIRECTORIES (measured on git
// 2.50.1, exit 2 in a clean environment). A restatement cannot be kept in step
// by review; these tests hold the seam that removes the reason to write one.
//
// EACH ASSERTION IS ABOUT AN ANSWER GIT GAVE, not about membership of a list —
// the shape historyvars_test.go's header warns against, which would recompute
// the fix the way the code does and stay green through any narrowing that
// narrowed both ends together. The one place a list IS compared
// (TestScrubEnvironAndScrubbedGitVarsDescribeTheSameSet) compares it against the
// names THIS FILE has measured values for, and refuses a policy name it has no
// value for, so a tenth variable fails here rather than passing untested.

package vcs_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/vcs"
)

// scrubProbeValues is a value for each scrubbed variable that makes it LIVE
// against the repository the fixtures build — pointed at another repository, so
// honouring it changes an answer, which is what makes the measurements below
// discriminating rather than decorative. GIT_NO_REPLACE_OBJECTS takes no path:
// it suppresses the repository's own replace refs, so its live spelling is any
// non-empty value.
func scrubProbeValues(other string) map[string]string {
	git := filepath.Join(other, ".git")
	objects := filepath.Join(git, "objects")
	return map[string]string{
		"GIT_DIR":                          git,
		"GIT_WORK_TREE":                    other,
		"GIT_COMMON_DIR":                   git,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": objects,
		"GIT_OBJECT_DIRECTORY":             objects,
		"GIT_GRAFT_FILE":                   filepath.Join(git, "info", "grafts"),
		"GIT_NO_REPLACE_OBJECTS":           "1",
		"GIT_REPLACE_REF_BASE":             "refs/alt-replace/",
		"GIT_SHALLOW_FILE":                 filepath.Join(git, "shallow"),
	}
}

// twoRepositories builds a repository and an unrelated one holding a commit the
// first does not, and returns both plus that foreign commit.
//
// The foreign commit's content is unique so the two fixtures cannot build
// byte-identical commits within the same second — without that, the "foreign"
// SHA is one this repository already holds and every measurement below resolves
// for a reason that has nothing to do with the environment.
func twoRepositories(t *testing.T) (mine, other, foreign string) {
	t.Helper()
	mine = initRepo(t)
	write(t, mine, "mine.txt", "mine\n")
	run(t, mine, "add", "-A")
	run(t, mine, "commit", "-q", "-m", "mine")

	other = initRepo(t)
	write(t, other, "only-here.txt", "unique to the other repository\n")
	run(t, other, "add", "-A")
	run(t, other, "commit", "-q", "-m", "only here")

	foreign = rev(t, other, "HEAD")
	if foreign == rev(t, mine, "HEAD") {
		t.Fatalf("the two fixture repositories share HEAD %s, so nothing here is foreign", foreign)
	}
	return mine, other, foreign
}

// THE OBJECT-STORE FAMILY IS THE HALF A RESTATEMENT DROPS, and it is the half no
// identity comparison can see — git resolves the repository the caller named and
// answers from another one's objects, so the git directory, the common directory
// and the work tree all come back byte-identical (#176).
//
// GIT_ALTERNATE_OBJECT_DIRECTORIES and GIT_OBJECT_DIRECTORY are asserted by name
// because they are the two measured LIVE against the shape #307 reproduces:
// under either of them `seat-checkout cut` exits 0 having materialised a
// worktree of a commit the named repository does not hold.
//
// The control run is not optional. It asserts the ambient environment really
// does answer from the other repository here, so a scrub that removed nothing
// could not pass by the variable having been inert on this machine.
func TestScrubEnvironRemovesTheObjectStoreFamilyForCallersOutsideThisPackage(t *testing.T) {
	mine, other, foreign := twoRepositories(t)
	probe := scrubProbeValues(other)

	for _, name := range []string{"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_OBJECT_DIRECTORY"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, probe[name])

			if err := gitErr(t, mine, os.Environ(), "cat-file", "-e", foreign); err != nil {
				t.Fatalf("control: under an ambient %s naming the other repository's object store, `git -C <mine> cat-file -e %s` failed (%v) — the variable is inert here, so this fixture cannot tell a scrub from a no-op", name, foreign, err)
			}

			kept, removed := vcs.ScrubEnviron(os.Environ())

			if err := gitErr(t, mine, kept, "cat-file", "-e", foreign); err == nil {
				t.Fatalf("vcs.ScrubEnviron returned an environment that still carries %s: `git -C <mine> cat-file -e %s` succeeded for a commit only the other repository holds, so a caller outside internal/vcs is answered from a repository nobody named", name, foreign)
			}
			if !slices.Contains(removed, name) {
				t.Fatalf("vcs.ScrubEnviron removed %s without reporting it (reported %v), so a caller cannot tell an operator which variable it dropped", name, removed)
			}
		})
	}
}

// THE POINTER FAMILY, and the split MovedRepository draws through it, measured
// rather than restated.
//
// MovedRepository is what arms the identity probe, so its contract is not "these
// three names" but "the names that actually move the repository git resolves".
// This asks git which those are — one identity question under the ambient
// environment and one under the environment vcs.ScrubEnviron returns — and
// requires the split to agree with the answer for every scrubbed variable.
//
// A history variable that ended up in MovedRepository would arm a probe that
// then compares two identical answers, at the cost of two forks per git call and
// a refusal message ("moves the repository git resolves") that is false of it. A
// pointer variable left OUT of it would disarm the probe that is the only thing
// standing between formwork and a repository nobody named.
func TestMovedRepositoryNamesExactlyTheScrubbedVariablesThatMoveGitsAnswer(t *testing.T) {
	mine, other, _ := twoRepositories(t)
	probe := scrubProbeValues(other)
	identity := []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir", "--show-toplevel"}

	names := vcs.ScrubbedGitVars()
	if len(names) == 0 {
		t.Fatalf("vcs.ScrubbedGitVars() named nothing, so there is no policy here to measure")
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("vcs.ScrubbedGitVars() = %v is not sorted, and operator-facing text names it in this order", names)
	}

	for _, name := range names {
		val, ok := probe[name]
		if !ok {
			t.Fatalf("the scrub policy names %s and this test has no live value for it, so whether it moves the repository git resolves is untested and MovedRepository's split over it is unproved", name)
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, val)

			kept, removed := vcs.ScrubEnviron(os.Environ())
			if !slices.Contains(removed, name) {
				t.Fatalf("vcs.ScrubEnviron did not remove %s, which the policy names: a caller outside internal/vcs runs git with it still set", name)
			}

			ambient := gitOut(t, mine, os.Environ(), identity...)
			scrubbed := gitOut(t, mine, kept, identity...)
			movesTheAnswer := ambient != scrubbed

			inMoved := slices.Contains(vcs.MovedRepository(removed), name)
			if inMoved != movesTheAnswer {
				t.Fatalf("vcs.MovedRepository(%v) %s %s, but git's own identity answer %s under it — ambient %q, scrubbed %q",
					removed,
					map[bool]string{true: "names", false: "does not name"}[inMoved], name,
					map[bool]string{true: "moves", false: "is byte-identical"}[movesTheAnswer],
					ambient, scrubbed)
			}
		})
	}
}

// THE ADVERTISED SET AND THE APPLIED SET MUST BE ONE SET. Operator-facing text
// names vcs.ScrubbedGitVars (env.go's refusal and GitEnvNotice both print it),
// and git sees whatever vcs.ScrubEnviron actually strips; a caller told one
// thing and given the other is the shape #307 is about, one package boundary
// further out.
//
// The expectation is the set of names this file holds a live value for, not a
// copy of the package's list, so narrowing both ends together — the way a
// restatement narrows — still fails here.
func TestScrubEnvironAndScrubbedGitVarsDescribeTheSameSet(t *testing.T) {
	_, other, _ := twoRepositories(t)
	probe := scrubProbeValues(other)
	want := slices.Sorted(maps.Keys(probe))

	for name, val := range probe {
		t.Setenv(name, val)
	}
	kept, removed := vcs.ScrubEnviron(os.Environ())

	if !slices.Equal(removed, want) {
		t.Errorf("with every scrubbed variable set, vcs.ScrubEnviron removed %v, want %v", removed, want)
	}
	if got := vcs.ScrubbedGitVars(); !slices.Equal(got, want) {
		t.Errorf("vcs.ScrubbedGitVars() = %v, want %v — this is the list operator-facing text prints, so it must be the list the scrub applies", got, want)
	}
	for _, kv := range kept {
		if name, _, ok := strings.Cut(kv, "="); ok && slices.Contains(want, name) {
			t.Errorf("vcs.ScrubEnviron kept %s in the environment it returned", name)
		}
	}
	if len(kept) == 0 {
		t.Errorf("vcs.ScrubEnviron kept nothing at all, so it is discarding the environment rather than scrubbing it")
	}
}
