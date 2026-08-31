package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// An added-rule set keyed on rule ID has a recall hole, found independently by
// two other streams tonight and confirmed here: a rule that ALREADY EXISTS and
// is EDITED into the undecidable class keeps its id on both sides of the diff,
// so it is not an addition and walks through on every future PR.
//
// That is not hypothetical for this class. Widening a glob is an ordinary
// refactor edit — "this moved, broaden the scope" — and #9992's history has four
// instances repaired by MOVING TYPE, which is the same one-line edit in the
// other direction. A rule can enter vacuity without ever being born.
//
// The fix is to compare the census's OWN decidability verdict at the diff's base
// against its verdict at head, and judge any rule that TRANSITIONED — absent or
// decidable at base, undecidable at head. That is exact in both directions:
// it catches the widening edit, and it charges nobody for a standing offender.
//
// Deliberately NOT a fingerprint over the rule's declaration. A fingerprint
// re-judges on any edit to the fields it covers, so editing a standing
// offender's cure text or its scope for an unrelated reason reds the PR on a
// defect the author did not write and may not be able to cure. That is the shape
// that gets a gate switched off. Reading the verdict itself has no such
// collateral: a rule that was already undecidable is still undecidable, the
// transition never fires, and the author is left alone.

// wideBase is a set-relation whose two sides are drawn from DISJOINT globs. The
// census can probe it, and does: it is decidable at base.
const wideBase = `rules:
  - id: widened-relation
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go', 'gen/**/*.go']
    params:
      a:
        files: ['src/base.go']
        pattern: 'const ([A-Za-z]+)'
        group: 1
      b:
        files: ['gen/**/*.go']
        pattern: 'const ([A-Za-z]+)'
        group: 1
      relation: subset
    tags: [always]
`

// wideHead is the SAME rule id after an ordinary-looking scope widening: the
// b-side now matches the a-side's own file, so blanking b blanks a with it and
// no edit to the tree can falsify the relation. The id is on both sides of the
// diff, so nothing keyed on addition can see this.
const wideHead = `rules:
  - id: widened-relation
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go', 'gen/**/*.go']
    params:
      a:
        files: ['src/base.go']
        pattern: 'const ([A-Za-z]+)'
        group: 1
      b:
        files: ['src/**/*.go', 'gen/**/*.go']
        pattern: 'const ([A-Za-z]+)'
        group: 1
      relation: subset
    tags: [always]
`

// wideSources give both sides cardinality 1 so EMPTY-SIDE stays quiet and the
// arm under test is the only thing that can speak.
func wideSources() map[string]string {
	return map[string]string{
		"src/base.go": "package a\n\nconst Alpha = 1\n",
		"gen/gen.go":  "package gen\n\nconst Alpha = 1\n",
	}
}

// An EDIT that pushes a decidable rule into the undecidable set must be refused,
// exactly as an addition would be. This is the recall hole stated as a test.
func TestWideningADecidableRuleIntoTheUndecidableSetIsRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	writeRules(t, root, "rel.yaml", wideBase)
	gitInit(t, root)
	base := commitAll(t, root, "base holds a DECIDABLE relation")

	writeRules(t, root, "rel.yaml", wideHead)
	commitAll(t, root, "widen the b side over the a side")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	code, out := census(t, root)
	if !strings.Contains(out, verdictLine) {
		t.Fatalf("a rule EDITED into the undecidable set was not refused:\n%s", out)
	}
	if !strings.Contains(out, "widened-relation") {
		t.Fatalf("the verdict does not name the rule it refuses:\n%s", out)
	}
	if !strings.Contains(out, editedSentence) {
		t.Fatalf("an EDITED rule was reported as an ADDED one — the author is told to look for a rule they never wrote:\n%s", out)
	}
	if code == 0 {
		t.Fatalf("census exited 0 with a NEW-RULE-UNDECIDED verdict:\n%s", out)
	}
}

// The two branches emit DIFFERENT sentences, and until this test existed nothing
// could tell them apart. A rule absent at base has `was == ""` by zero value, so
// the transition branch swallows additions too: deleting the addition branch
// entirely left every other test in this package green. That is a new branch
// shipping untested — mutate each arm separately or the second one is decoration.
//
// The sentences are the observable difference, so they are what the tests assert.
// It is not cosmetic: telling an author who ADDED a rule that they "edited it out
// of the decidable set" sends them diffing a base that never contained it.
const addedSentence = "this change ADDS this rule"
const editedSentence = "this change EDITS this rule OUT of the set the census can decide"

// The addition branch, pinned by the sentence only it produces.
func TestAnAddedUndecidableRuleIsReportedAsAddedNotEdited(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", overlappingRelation)
	commitAll(t, root, "add an undecidable rule")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if !strings.Contains(out, addedSentence) {
		t.Fatalf("an ADDED rule was not reported as added:\n%s", out)
	}
	if strings.Contains(out, editedSentence) {
		t.Fatalf("an ADDED rule was reported as an EDIT of a rule that never existed at base:\n%s", out)
	}
}

// The precision half, and the reason this reads the VERDICT rather than a
// fingerprint over the declaration. A rule that was ALREADY undecidable at base
// is one of the backfill; editing its cure text changes nothing about whether it
// can fire, and charging the author for it would red a PR on a defect they did
// not write. A fingerprint over the rule's text fires here. Reading the
// transition does not.
func TestEditingAStandingOffendersCureIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	writeRules(t, root, "rel.yaml", overlappingRelation)
	gitInit(t, root)
	base := commitAll(t, root, "base already holds the undecidable rule")

	writeRules(t, root, "rel.yaml",
		strings.Replace(overlappingRelation, "    tags: [always]",
			"    cure: \"A newly written cure sentence.\"\n    tags: [always]", 1))
	commitAll(t, root, "reword the cure")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, verdictLine) {
		t.Fatalf("a standing offender was charged to an author who only reworded its cure:\n%s", out)
	}
}

// baseUndecidedReasons is the arm's only reader of the BASE tree, and its
// contract is what keeps the transition honest: a rule absent at base must be
// absent from the map, NOT present with an empty reason. Conflating the two
// would make every newly added rule look like it had been decidable before,
// which is the same silent pass this arm exists to close.
func TestBaseUndecidedReasonsSeparatesAbsentFromDecidable(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	writeRules(t, root, "rel.yaml", wideBase)
	gitInit(t, root)
	base := commitAll(t, root, "base holds a decidable relation")

	writeRules(t, root, "new.yaml", overlappingRelation)
	commitAll(t, root, "add an undecidable one")

	reasons, err := baseUndecidedReasons(root, base+"..HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reasons["widened-relation"]; !ok {
		t.Fatal("a rule present at base is missing from the base map")
	}
	if reasons["widened-relation"].reason != "" {
		t.Fatalf("a DECIDABLE rule carries a reason at base: %q", reasons["widened-relation"].reason)
	}
	if _, ok := reasons["new-overlapping-relation"]; ok {
		t.Fatal("a rule the change ADDS is present in the base map — absent and decidable must not be conflated")
	}
}

// A base tree with no .formwork/ at all is the first commit that introduces the
// corpus. Every rule is then new, and none may be read as previously decidable.
func TestBaseWithNoCorpusYieldsNoReasons(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rulesDir := filepath.Join(root, ".formwork")
	moved := filepath.Join(root, "stash-formwork")
	if err := os.Rename(rulesDir, moved); err != nil {
		t.Fatal(err)
	}
	base := commitAll(t, root, "a tree with no corpus yet")
	if err := os.Rename(moved, rulesDir); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "introduce the corpus")

	reasons, err := baseUndecidedReasons(root, base+"..HEAD")
	if err != nil {
		t.Fatalf("a base with no corpus must not be an error: %v", err)
	}
	if len(reasons) != 0 {
		t.Fatalf("a base with no corpus yielded %d reasons", len(reasons))
	}
}

// gitOnlyPATH is a PATH holding git and nothing else. It states the portability
// invariant below as an environment rather than as a string match, so a rewrite
// that swaps `tar` for any other general-purpose CLI is caught by the same test.
func gitOnlyPATH(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is the census's data source and must be on PATH: %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(git, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// exportPath must not need a `tar` binary.
//
// This repo closed the GNU/BSD CLI-divergence class deliberately: the last four
// product shells were ported to Go and DELETED (#15103 / #14888), tracked *.sh
// is zero, and no-shell-files-anywhere plus tracked-sh-shrink-only hold it shut.
// A `tar` invocation inside a .go file re-enters that class through a door none
// of those rules watch, and BSD/GNU tar divergence presents as a macOS-only
// failure — which is exactly the shape #16179 was reported as.
//
// Asserted by REMOVING tar from the environment rather than by grepping the
// source for it: the test then pins the property (this runs anywhere git runs)
// instead of one spelling of the defect.
func TestExportPathNeedsNoTarBinary(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	gitInit(t, root)
	rev := commitAll(t, root, "a corpus to export")

	t.Setenv("PATH", gitOnlyPATH(t))
	dst := t.TempDir()
	if err := exportPath(root, rev, ".formwork", dst); err != nil {
		t.Fatalf("exportPath cannot run without a tar binary on PATH: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".formwork", "rules", "test.yaml")); err != nil {
		t.Fatalf("the rule corpus is not in the exported tree: %v", err)
	}
}

// The export must carry what its readers open, and nothing else.
//
// config.Load reads .formwork/formwork.yaml, .formwork/rules/*.yaml and the
// allowlists those rules name; loadRuleMeta and existsDeclarations glob
// .formwork/rules/*.yaml. Nothing in that path opens a fixture or a mutation
// spec — yet on the real corpus those are 18,392 of the 20,048 files the export
// writes to disk. The function's own comment ("~1500 small files") is an
// accurate description of the WORKING SET; it is the export that outgrew it.
func TestExportPathCarriesOnlyWhatItsReadersOpen(t *testing.T) {
	files := wideSources()
	files[".formwork/allowlists/widened-relation.txt"] = "# no entries\n"
	files[".formwork/fixtures/widened-relation/pass-1/src/base.go"] = "package a\n"
	files[".formwork/mutations/widened-relation.yaml"] = "spec: {}\n"
	root := writeCorpus(t, baseRules, files)
	gitInit(t, root)
	rev := commitAll(t, root, "a corpus with fixtures and mutations")

	dst := t.TempDir()
	if err := exportPath(root, rev, ".formwork", dst); err != nil {
		t.Fatal(err)
	}
	for _, opened := range []string{
		filepath.Join(".formwork", "formwork.yaml"),
		filepath.Join(".formwork", "rules", "test.yaml"),
		filepath.Join(".formwork", "allowlists", "widened-relation.txt"),
	} {
		if _, err := os.Stat(filepath.Join(dst, opened)); err != nil {
			t.Fatalf("%s is absent from the exported tree — config.Load, loadRuleMeta or existsDeclarations reads it: %v", opened, err)
		}
	}
	for _, unread := range []string{
		filepath.Join(".formwork", "fixtures", "widened-relation", "pass-1", "src", "base.go"),
		filepath.Join(".formwork", "mutations", "widened-relation.yaml"),
	} {
		if _, err := os.Stat(filepath.Join(dst, unread)); err == nil {
			t.Fatalf("%s was materialised: the export writes files no reader of it ever opens", unread)
		}
	}
}

// A corpus with no allowlists/ must still export.
//
// The trap in narrowing the pathspec: `git archive` treats a pathspec matching
// nothing as FATAL (exit 128), so a hardcoded member list refuses every rev that
// lacks one member — including the commit that introduces allowlists, and every
// corpus writeCorpus builds. Each member is probed before it is asked for.
func TestExportPathToleratesACorpusWithoutAllowlists(t *testing.T) {
	root := writeCorpus(t, baseRules, wideSources())
	gitInit(t, root)
	rev := commitAll(t, root, "a corpus with no allowlists")

	if _, err := os.Stat(filepath.Join(root, ".formwork", "allowlists")); !os.IsNotExist(err) {
		t.Fatalf("this fixture must have NO allowlists dir or the trap is untested: %v", err)
	}
	dst := t.TempDir()
	if err := exportPath(root, rev, ".formwork", dst); err != nil {
		t.Fatalf("a corpus without allowlists/ was refused: %v", err)
	}
}
