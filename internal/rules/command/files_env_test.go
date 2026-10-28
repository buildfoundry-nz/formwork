package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// FORMWORK_FILES names a file listing, one root-relative path per line, the
// changed in-scope files a --staged or --range run handed this rule. A
// detector that judges one file at a time can read it and parse nothing else.
// A whole-tree run sets nothing, and a detector that ignores it still judges
// the root, as before.

func finalizeChangeset(t *testing.T, c rules.Checker) ([]rules.Match, error) {
	t.Helper()
	return c.(rules.ErrFinalizer).FinalizeErr(rules.FinalizeContext{Root: t.TempDir(), Changeset: true})
}

const listFiles = `cmd: [sh, -c, 'printf "[%s]" "$(cat "$FORMWORK_FILES")"; exit 1']`

func TestCommandListsTheFilesItWasGiven(t *testing.T) {
	c := build(t, listFiles)
	for _, p := range []string{"b/two.go", "a/one.go", "b/two.go"} {
		if _, err := c.CheckFile(scan.NewMemFile(p, nil)); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := finalizeChangeset(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "[a/one.go\nb/two.go]") {
		t.Fatalf("the listing is every file given, sorted and once each: %+v", ms)
	}
}

func TestCommandListsAnEmptySetAsAnEmptyFile(t *testing.T) {
	ms, err := finalizeChangeset(t, build(t, listFiles))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "[]") {
		t.Fatalf("no file given is an empty listing, never an unset variable: %+v", ms)
	}
}

func TestCommandRemovesTheListingAfterTheRun(t *testing.T) {
	c := build(t, `cmd: [sh, -c, 'printf "%s" "$FORMWORK_FILES"; exit 1']`)
	if _, err := c.CheckFile(scan.NewMemFile("a/one.go", nil)); err != nil {
		t.Fatal(err)
	}
	ms, err := finalizeChangeset(t, c)
	if err != nil || len(ms) != 1 {
		t.Fatalf("finalize: %v %+v", err, ms)
	}
	i := strings.Index(ms[0].Message, "/")
	if i < 0 {
		t.Fatalf("no listing path in %q", ms[0].Message)
	}
	path := strings.Fields(ms[0].Message[i:])[0]
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the listing %s outlived the run: %v", path, err)
	}
}

func TestWholeTreeRunListsNothing(t *testing.T) {
	c := build(t, `cmd: [sh, -c, 'printf "<%s>" "${FORMWORK_FILES-unset}"; exit 1']`)
	if _, err := c.CheckFile(scan.NewMemFile("a/one.go", nil)); err != nil {
		t.Fatal(err)
	}
	ms, err := finalize(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "<unset>") {
		t.Fatalf("a whole-tree run judges the root and lists nothing: %+v", ms)
	}
}
