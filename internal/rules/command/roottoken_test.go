// roottoken_test.go — the engine owns the tree a detector reads (#28).
//
// A command rule used to name its tree with a path relative to the caller's
// working directory (`--root ../../..`). Under `check` that resolved to the
// repository, which is what the author meant; under `test` the fixture runner
// makes the fixture tree the cwd, so the same argv resolved to the repository
// as well, and the fixture judged the real tree. The engine already knows
// which tree it is evaluating, so it substitutes it: {{root}} is the tree
// under evaluation, {{repo}} the corpus's own tree, and a detector can no
// longer express "some directory relative to wherever I was started".
package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"gopkg.in/yaml.v3"
)

// finalizeIn runs the rule with an explicit pair of roots, which is what the
// two planes differ in: under check they are the same directory, under test
// root is the fixture tree and repo is the corpus.
func finalizeIn(t *testing.T, c rules.Checker, root, repo string) ([]rules.Match, error) {
	t.Helper()
	ef, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("command should implement ErrFinalizer")
	}
	return ef.FinalizeErr(rules.FinalizeContext{Root: root, Repo: repo})
}

// The tokens are substituted with the absolute paths of the two trees. The
// detector writes what it was handed, and the test reads it back: a
// substitution that produced the wrong path would be invisible to an
// exit-code assertion.
func TestRootAndRepoTokensAreSubstituted(t *testing.T) {
	root, repo := t.TempDir(), t.TempDir()
	out := filepath.Join(root, "seen.txt")
	c := build(t, "cmd: [sh, -c, 'printf \"%s\\n%s\\n\" \"$1\" \"$2\" > \"$3\"', sh, '{{root}}', '{{repo}}', '"+out+"']")
	if m, err := finalizeIn(t, c, root, repo); err != nil || len(m) != 0 {
		t.Fatalf("expected pass, got matches=%v err=%v", m, err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := root + "\n" + repo + "\n"
	if string(b) != want {
		t.Fatalf("tokens resolved to %q, want %q", b, want)
	}
}

// A token inside a longer argument is substituted in place: the canonical
// shape is `go -C {{repo}}/scripts/dev/x run . --root {{root}}`, where the
// token is a prefix of the argument rather than the whole of it.
func TestTokenIsSubstitutedInsideAnArgument(t *testing.T) {
	root, repo := t.TempDir(), t.TempDir()
	out := filepath.Join(root, "seen.txt")
	c := build(t, "cmd: [sh, -c, 'printf \"%s\" \"$1\" > \"$2\"', sh, '{{repo}}/scripts/dev/x', '"+out+"']")
	if m, err := finalizeIn(t, c, root, repo); err != nil || len(m) != 0 {
		t.Fatalf("expected pass, got matches=%v err=%v", m, err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repo, "scripts", "dev", "x"); string(b) != want {
		t.Fatalf("token inside an argument resolved to %q, want %q", b, want)
	}
}

// A corpus that names no repo still runs: {{repo}} falls back to the tree
// under evaluation, which is what the two are under `check`. Without this a
// caller that predates the field would substitute an empty path and the
// detector would read the filesystem root.
func TestRepoFallsBackToRootWhenUnset(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "seen.txt")
	c := build(t, "cmd: [sh, -c, 'printf \"%s\" \"$1\" > \"$2\"', sh, '{{repo}}', '"+out+"']")
	if m, err := finalizeIn(t, c, root, ""); err != nil || len(m) != 0 {
		t.Fatalf("expected pass, got matches=%v err=%v", m, err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != root {
		t.Fatalf("{{repo}} with no repo set resolved to %q, want the tree under evaluation %q", b, root)
	}
}

// A `..` segment in argv is refused at LOAD, not reported at run: once a rule
// can name its tree exactly, naming it relatively has no legitimate use, and a
// rule that does not load cannot read the wrong tree even once.
func TestParentSegmentInArgvIsRefusedAtLoad(t *testing.T) {
	for _, argv := range []string{
		"cmd: [go, -C, scripts/dev/x, run, ., --root, ../../..]",
		"cmd: [go, run, -C, ../tools/x, .]",
		"cmd: [sh, -c, 'true', sh, ..]",
	} {
		f, ok := rules.Lookup("command")
		if !ok {
			t.Fatal("command type not registered")
		}
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(argv), &doc); err != nil {
			t.Fatal(err)
		}
		_, err := f(doc.Content[0])
		if err == nil {
			t.Fatalf("%s: loaded; a parent segment in argv must be refused", argv)
		}
		if !strings.Contains(err.Error(), "{{root}}") {
			t.Fatalf("%s: refusal does not name the cure: %v", argv, err)
		}
	}
}

// A `..` inside a value that is not a path must still load: the refusal is
// about path segments, and a regex or a message legitimately carries dots.
func TestNonPathDotsStillLoad(t *testing.T) {
	for _, argv := range []string{
		"cmd: [sh, -c, 'grep -E \"a..b\" f']",
		"cmd: [go, run, ., --pattern, 'x...y']",
	} {
		f, _ := rules.Lookup("command")
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(argv), &doc); err != nil {
			t.Fatal(err)
		}
		if _, err := f(doc.Content[0]); err != nil {
			t.Fatalf("%s: refused, but it names no parent directory: %v", argv, err)
		}
	}
}

// A detector judging a FIXTURE is told so, because some planes exist only in
// the live repository and a fixture cannot fake them (#28). A commit-range
// scan is the case: an isolated fixture is a real repository, so "is this a
// git checkout" answers yes, but it has no upstream branch for a default
// range to resolve against — and a detector that cannot tell the planes apart
// either dies on the missing ref or skips the range in CI too, which is a
// gate that fails open.
func TestAFixtureRunIsDeclaredToTheDetector(t *testing.T) {
	root, repo := t.TempDir(), t.TempDir()
	out := filepath.Join(root, "seen.txt")
	c := build(t, "cmd: [sh, -c, 'printf \"%s\" \"${FORMWORK_FIXTURE:-unset}\" > \"$1\"', sh, '"+out+"']")
	ef := c.(rules.ErrFinalizer)
	if _, err := ef.FinalizeErr(rules.FinalizeContext{Root: root, Repo: repo, Fixture: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "1" {
		t.Fatalf("a fixture run must declare itself, got %q", b)
	}
	if _, err := ef.FinalizeErr(rules.FinalizeContext{Root: root, Repo: root}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "unset" {
		t.Fatalf("a check run must NOT declare itself a fixture, got %q — a detector would skip its range plane in CI", b)
	}
}

// Both tokens resolve to ABSOLUTE paths, whatever the caller spelled.
//
// `formwork test -C .` makes the corpus root the literal ".", and a relative
// token is resolved by the CHILD against its own working directory — which is
// the tree under evaluation, not the corpus. {{repo}} then pointed back at the
// fixture: measured downstream as `go: cannot find main module, but found
// .git/config in <the fixture>`, a detector looking for its own module inside
// the tree it was judging.
func TestTokensResolveToAbsolutePaths(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	out := filepath.Join(root, "seen.txt")
	c := build(t, "cmd: [sh, -c, 'printf \"%s\\n%s\\n\" \"$1\" \"$2\" > \"$3\"', sh, '{{root}}', '{{repo}}', '"+out+"']")
	// The shapes a CLI actually produces: -C . and a relative subdirectory.
	if _, err := finalizeIn(t, c, root, "."); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want two paths, got %q", b)
	}
	for _, got := range lines {
		if !filepath.IsAbs(got) {
			t.Fatalf("token resolved to %q, which the child resolves against ITS OWN working directory — the tree under evaluation, not the corpus", got)
		}
	}
	if lines[1] != wd {
		t.Fatalf("{{repo}} for a corpus rooted at \".\" resolved to %q, want the process working directory %q", lines[1], wd)
	}
}
