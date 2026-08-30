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

// #176: THE OBJECT-STORE AND HISTORY FAMILY MOVES WHAT A RANGE *IS*.
//
// #167 scrubbed the repository POINTERS — git still resolves the right
// repository under these, and answers about a different history inside it. The
// failure direction is silent and toward passing: a range that resolves to
// different commits reports different findings, and `check --range` exits 0
// having judged a changeset nobody named. `--range` is the CI-facing surface,
// which is exactly where an ambient environment is set by something other than
// the person reading the result.
//
// Each test below asserts the ANSWER, not the scrub list: the range must resolve
// to what a clean environment resolves it to. Asserting membership of
// scrubbedGitVars would recompute the fix the way the code does — the
// tautological shape the mutation discipline exists to catch.

// GIT_GRAFT_FILE rewrites commit parentage for the process that reads it.
// Measured on git 2.50.1: `git diff --name-only HEAD~1..HEAD` answered f3.txt
// plain and `f2.txt f3.txt` with a graft naming HEAD's grandparent as its parent.
// Both range readers named in the issue are asserted — RangePaths behind
// `check --range`, and Diff behind every git-diff rule.
func TestAnAmbientGraftFileDoesNotMoveARange(t *testing.T) {
	dir := threeCommitRepo(t)
	grafts := filepath.Join(t.TempDir(), "grafts")
	writeFile(t, grafts, rev(t, dir, "HEAD")+" "+rev(t, dir, "HEAD~2")+"\n")
	t.Setenv("GIT_GRAFT_FILE", grafts)

	// The control: with the variable in force git really does answer differently,
	// so a green here cannot be "this environment changes nothing".
	if got := gitLines(t, dir, os.Environ(), "diff", "--name-only", "HEAD~1..HEAD"); !reflect.DeepEqual(got, []string{"f2.txt", "f3.txt"}) {
		t.Fatalf("control: git under GIT_GRAFT_FILE answered %v, want [f2.txt f3.txt] — the fixture no longer reproduces #176", got)
	}

	got, err := vcs.RangePaths(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"f3.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths = %v, want %v — the graft file moved the changeset formwork judged", got, want)
	}
	diff, err := vcs.Diff(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(diff, "f2.txt") {
		t.Errorf("Diff carries f2.txt, which is not in this range:\n%s", diff)
	}
}

// GIT_REPLACE_REF_BASE points git at a different set of replace refs, reaching
// the same history the same way. Measured on git 2.50.1: with a replacement
// commit stored under refs/alt-replace/, the same range answered f3.txt plain
// and `f2.txt f3.txt` with the base named.
func TestAnAmbientReplaceRefBaseDoesNotMoveARange(t *testing.T) {
	dir := threeCommitRepo(t)
	replaceHeadsParent(t, dir, "refs/alt-replace/")
	t.Setenv("GIT_REPLACE_REF_BASE", "refs/alt-replace/")

	if got := gitLines(t, dir, os.Environ(), "diff", "--name-only", "HEAD~1..HEAD"); !reflect.DeepEqual(got, []string{"f2.txt", "f3.txt"}) {
		t.Fatalf("control: git under GIT_REPLACE_REF_BASE answered %v, want [f2.txt f3.txt]", got)
	}

	got, err := vcs.RangePaths(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"f3.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths = %v, want %v", got, want)
	}
}

// GIT_NO_REPLACE_OBJECTS runs the family in the other direction: it SUPPRESSES
// replace refs the repository itself declares, so the history formwork judges is
// narrower than the one every other git command in that repository sees.
// Measured on git 2.50.1: with a refs/replace/ entry present, the range answered
// `f2.txt f3.txt`, and f3.txt alone with the variable set.
func TestAmbientNoReplaceObjectsDoesNotSuppressTheRepositorysReplaceRefs(t *testing.T) {
	dir := threeCommitRepo(t)
	replaceHeadsParent(t, dir, "refs/replace/")
	t.Setenv("GIT_NO_REPLACE_OBJECTS", "1")

	if got := gitLines(t, dir, os.Environ(), "diff", "--name-only", "HEAD~1..HEAD"); !reflect.DeepEqual(got, []string{"f3.txt"}) {
		t.Fatalf("control: git under GIT_NO_REPLACE_OBJECTS answered %v, want [f3.txt]", got)
	}

	got, err := vcs.RangePaths(dir, "HEAD~1..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"f2.txt", "f3.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RangePaths = %v, want %v — the repository's own replace ref was suppressed from the environment", got, want)
	}
}

// GIT_OBJECT_DIRECTORY and GIT_SHALLOW_FILE reach the same history through the
// object store and the shallow boundary. Both are asserted through their LOUD
// spelling — pointed at an empty object directory, and at a shallow boundary that
// cuts the range's own base, git cannot resolve the range at all (measured,
// `fatal: ambiguous argument`). The assertion is still about the answer: under
// the scrub the range resolves normally, so an environment that decides whether
// formwork can answer at all no longer decides it.
func TestAmbientObjectStoreVariablesDoNotDecideWhetherARangeResolves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value func(t *testing.T, dir string) string
		rng   string
		want  []string
	}{
		{
			name:  "GIT_OBJECT_DIRECTORY",
			value: func(t *testing.T, _ string) string { return t.TempDir() },
			rng:   "HEAD~1..HEAD",
			want:  []string{"f3.txt"},
		},
		{
			name: "GIT_SHALLOW_FILE",
			value: func(t *testing.T, dir string) string {
				p := filepath.Join(t.TempDir(), "shallow")
				writeFile(t, p, rev(t, dir, "HEAD~1")+"\n")
				return p
			},
			rng:  "HEAD~2..HEAD",
			want: []string{"f2.txt", "f3.txt"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := threeCommitRepo(t)
			t.Setenv(tc.name, tc.value(t, dir))

			// The control: with it in force, git cannot answer at all.
			if err := gitErr(t, dir, os.Environ(), "diff", "--name-only", tc.rng); err == nil {
				t.Fatalf("control: git under %s answered the range, so this fixture proves nothing", tc.name)
			}

			got, err := vcs.RangePaths(dir, tc.rng)
			if err != nil {
				t.Fatalf("RangePaths under %s: %v", tc.name, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("RangePaths = %v, want %v", got, tc.want)
			}
		})
	}
}

// GIT_ALTERNATE_OBJECT_DIRECTORIES ADDS an object store, so its direction is the
// opposite of the others: it makes objects from a repository nobody named
// resolvable inside this one. formwork must answer about the repository's own
// object store — which here means REFUSING a range whose base exists only in the
// other repository, rather than reporting a diff against a foreign commit.
func TestAmbientAlternateObjectDirectoriesDoNotAnswerFromAnotherRepository(t *testing.T) {
	dir := threeCommitRepo(t)
	other := threeCommitRepo(t)
	// A commit whose content is unique to this repository. Without it the two
	// fixtures can build byte-identical commits within the same second, the
	// "foreign" SHA is then one this repository holds, and the range resolves for
	// a reason that has nothing to do with the alternate store.
	write(t, other, "only-here.txt", "unique to the other repository\n")
	run(t, other, "add", "-A")
	run(t, other, "commit", "-q", "-m", "only here")
	foreign := rev(t, other, "HEAD")
	if foreign == rev(t, dir, "HEAD") {
		t.Fatalf("the two fixture repositories share HEAD %s, so nothing here is foreign", foreign)
	}
	t.Setenv("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(other, ".git", "objects"))
	rng := foreign + "..HEAD"

	if err := gitErr(t, dir, os.Environ(), "diff", "--name-only", rng); err != nil {
		t.Fatalf("control: git under GIT_ALTERNATE_OBJECT_DIRECTORIES could not resolve the foreign base (%v), so this fixture proves nothing", err)
	}

	if got, err := vcs.RangePaths(dir, rng); err == nil {
		t.Fatalf("RangePaths over a base only the alternate object store holds = %v with no error, want a refusal", got)
	}
}

// threeCommitRepo commits f1, f2 and f3 in three commits, so HEAD~1..HEAD is one
// file and any rewriting of HEAD's parentage widens it to two.
func threeCommitRepo(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	for _, f := range []string{"f1.txt", "f2.txt", "f3.txt"} {
		write(t, dir, f, f+"\n")
		run(t, dir, "add", "-A")
		run(t, dir, "commit", "-q", "-m", f)
	}
	return dir
}

// replaceHeadsParent stores, under base, a replacement for HEAD whose parent is
// HEAD~2 — the replace-ref spelling of the graft above.
func replaceHeadsParent(t *testing.T, dir, base string) {
	t.Helper()
	tree := rev(t, dir, "HEAD^{tree}")
	replacement := strings.TrimSpace(gitOut(t, dir, os.Environ(), "commit-tree", tree, "-p", rev(t, dir, "HEAD~2"), "-m", "replaced"))
	run(t, dir, "update-ref", base+rev(t, dir, "HEAD"), replacement)
}

func rev(t *testing.T, dir, spec string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, dir, os.Environ(), "rev-parse", spec))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitOut runs git with an explicit environment and fails the test on any error —
// the control runs need git's answer under the ambient environment, which the
// package's own helpers deliberately no longer give.
func gitOut(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func gitLines(t *testing.T, dir string, env []string, args ...string) []string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(gitOut(t, dir, env, args...), "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func gitErr(t *testing.T, dir string, env []string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	_, err := cmd.Output()
	return err
}
