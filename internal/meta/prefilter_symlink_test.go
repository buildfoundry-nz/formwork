// prefilter_symlink_test.go — #143 row 4, lint's half.
//
// prefilter-load-bearing's fixture differential collects the rule's fixture
// dirs by name and diffs the rule against its prefilter-stripped twin over
// each one. os.ReadDir reports a symlink by its own lstat, so a symlinked
// fire-* tree had IsDir() false and dropped out of that collection — silently.
// The differential then judged a smaller set of evidence than the corpus
// declares, and the verdict it printed was about the trees it happened to
// enter, not the trees that exist.
//
// That matters here in a way it does not in `formwork test`: the fixture
// evidence is the ONLY arm this check has for a tombstone rule (#133), whose
// real-tree differential is empty on both sides by construction. Lose the one
// fixture that carries the dropped branch and a load-bearing prefilter reports
// clean. `formwork test` refusing the same tree does not cover this, because
// `formwork lint` is a separate command an operator can run alone.
package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintRefusesASymlinkedFixtureDirRatherThanJudgingWithoutIt(t *testing.T) {
	files := withTombstoneFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  tombstoneRule,
	})
	root := writeRepo(t, files)

	// The symlink target must live OUTSIDE the repo. Parked inside it, the
	// real-tree differential finds the banned name itself, proves the rule on
	// arm 1, and short-circuits before the fixture arm is ever consulted — so
	// the test would pass for a reason that has nothing to do with the fixture
	// collection under test.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "b.go"), []byte("package p // beta-two want: no-ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// fire-2 is the fixture carrying the branch the prefilter drops — the one
	// piece of evidence that makes this prefilter load-bearing. Replace it with
	// a symlink to an identical tree.
	real2 := filepath.Join(root, ".formwork", "fixtures", "no-ghost", "fire-2")
	if err := os.RemoveAll(real2); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, real2); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	_, out, err := lintRootErr(t, root)
	if err == nil {
		t.Fatalf("a symlinked fixture dir must be refused, not quietly dropped from the differential; output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "fire-2") {
		t.Fatalf("the refusal must name the entry it could not enter, got: %v", err)
	}
	if strings.Contains(out, "[prefilter-load-bearing] OK") {
		t.Fatalf("the check must not report a verdict it reached without its evidence:\n%s", out)
	}
}
