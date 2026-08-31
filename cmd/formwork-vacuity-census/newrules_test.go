package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The census renders NO verdict on two whole populations and says so out loud:
// set-relations it cannot probe, and symbol-anchored rules whose anchor names
// another package. A declined verdict is currently indistinguishable from a
// pass — the census exits 0 and nobody ever asked whether the rule can fire.
// Measured on the corpus at 952de0fe4c that is 19 of 90 set-relation rules and
// 38 of 49 symbol-anchored rules, 57 rules in all (#15837).
//
// These tests pin the arm that refuses to let a NEW one in: a rule this change
// ADDS must be one the census can decide. They are deliberately scoped to the
// ADDED set — the 57 already here are the backfill, and a whole-corpus form
// would block every PR until that set is empty.

// gitInit makes root a git checkout. Identity is passed per-command so nothing
// is written into the repo's config: a repo-local user.email silently
// de-attributes commits, and these throwaway trees must not teach that shape.
func gitInit(t *testing.T, root string) {
	t.Helper()
	git(t, root, "init", "-q")
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-C", root,
		"-c", "user.email=census@test.invalid",
		"-c", "user.name=census test",
		"-c", "commit.gpgsign=false",
	}, args...)
	out, err := gitCmd(full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitAll stages everything under root and commits it, returning the sha.
func commitAll(t *testing.T, root, msg string) string {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", msg)
	return git(t, root, "rev-parse", "HEAD")
}

// writeRules adds a second rule file to an existing corpus, so a test can put a
// rule in the BASE commit and another in the change under judgement.
func writeRules(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, ".formwork", "rules", name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// baseRules is one healthy rule, so the base commit has a corpus and the arm's
// verdicts are never the only thing in the output.
const baseRules = `rules:
  - id: base-forbidden
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`

// baseFiles carries the const the relation fixtures extract, so BOTH sides have
// cardinality 1 and EMPTY-SIDE stays quiet. The overlapping-sides rule below is
// then undecidable for exactly one reason — its globs — and the arm under test
// is the only thing that can speak about it.
const baseFiles = "package a\n\nconst Alpha = 1\n"

// overlappingRelation is the shape this campaign found BY HAND twice: the
// A-side file is itself matched by the B-side glob, so A ⊆ B holds by
// construction and no edit to the tree can falsify it. sidesAreSeparable
// returns false, so the census claims nothing about it.
const overlappingRelation = `rules:
  - id: new-overlapping-relation
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      a:
        files: ['src/base.go']
        pattern: 'const ([A-Za-z]+)'
        group: 1
      b:
        files: ['src/**/*.go']
        pattern: 'const ([A-Za-z]+)'
        group: 1
      relation: subset
    tags: [always]
`

// separableRelation is the same rule with the two sides pulled apart: blanking
// the b side leaves the a side standing, so the census can probe it and does.
const separableRelation = `rules:
  - id: new-separable-relation
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

// qualifiedAnchor names a symbol in ANOTHER package. The census has no type
// information to resolve it, so an absent call site cannot prove the symbol
// unreachable and no verdict is claimed.
// verdictLine is the RENDERED finding — `  <id> [CODE] <why>` — not the bare
// code. The distinction is load-bearing and these tests found it the hard way:
// report()'s own FAIL prose explains what a NEW-RULE-UNDECIDED finding means,
// and that sentence prints on ANY census failure. Every corpus below fails
// class 3 (a two-rule tree carries no fixtures), so a bracket-free
// `Contains(out, "NEW-RULE-UNDECIDED")` matched the cure text in all eight
// tests: the four positive ones passed without the arm existing, and the four
// over-firing guards could never have caught an arm that fired. Asserting the
// bracketed form is what makes them assertions about a verdict.
const verdictLine = " [NEW-RULE-UNDECIDED] "

const qualifiedAnchor = `rules:
  - id: new-qualified-anchor
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: 'otherpkg\.Fn'
      allowed_func: '^Allowed$'
    tags: [always]
`

// A set-relation whose two sides are drawn from overlapping globs is one the
// census declines to decide. Adding it must fail, and the failure must name the
// rule and say WHY it could not be decided — an author refused with no
// actionable reason routes around the gate.
func TestNewSetRelationWithOverlappingSidesGates(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", overlappingRelation)
	commitAll(t, root, "add the rule")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	code, out := census(t, root)
	if !strings.Contains(out, verdictLine) {
		t.Fatalf("no NEW-RULE-UNDECIDED verdict in output:\n%s", out)
	}
	if !strings.Contains(out, "new-overlapping-relation") {
		t.Fatalf("the verdict does not name the rule it refuses:\n%s", out)
	}
	if !strings.Contains(out, "OVERLAPPING") {
		t.Fatalf("the verdict does not say WHY it could not be decided:\n%s", out)
	}
	if code == 0 {
		t.Fatalf("census exited 0 with a NEW-RULE-UNDECIDED verdict:\n%s", out)
	}
}

// A gate that fails everything is not a gate. The same rule with its two sides
// pulled apart is decidable, so the arm must stay silent about it.
func TestNewSetRelationWithSeparableSidesIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{
		"src/base.go": "package a\n\nconst Alpha = 1\n",
		"gen/gen.go":  "package gen\n\nconst Alpha = 1\n",
	})
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", separableRelation)
	commitAll(t, root, "add the rule")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, verdictLine) {
		t.Fatalf("a decidable new rule was refused as undecided:\n%s", out)
	}
}

// A symbol anchor naming another package is the second declined population, and
// it is the larger one: 38 of 49 on the real corpus.
func TestNewSymbolAnchoredRuleWithQualifiedAnchorGates(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", qualifiedAnchor)
	commitAll(t, root, "add the rule")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	code, out := census(t, root)
	if !strings.Contains(out, verdictLine) {
		t.Fatalf("no NEW-RULE-UNDECIDED verdict in output:\n%s", out)
	}
	if !strings.Contains(out, "new-qualified-anchor") {
		t.Fatalf("the verdict does not name the rule it refuses:\n%s", out)
	}
	if !strings.Contains(out, "package-qualified") {
		t.Fatalf("the verdict does not say WHY it could not be decided:\n%s", out)
	}
	if code == 0 {
		t.Fatalf("census exited 0 with a NEW-RULE-UNDECIDED verdict:\n%s", out)
	}
}

// The arm is scoped to the ADDED set on purpose. An undecidable rule that was
// already here is the #15837 backfill, not this author's doing, and gating it
// would block every PR until the backfill is finished.
func TestUndecidableRuleAlreadyInTheBaseIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	writeRules(t, root, "new.yaml", overlappingRelation)
	gitInit(t, root)
	base := commitAll(t, root, "base holds the undecidable rule already")

	if err := os.WriteFile(filepath.Join(root, "src", "other.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "an unrelated change")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, verdictLine) {
		t.Fatalf("a rule the change did not add was refused as new:\n%s", out)
	}
}

// Moving a rule between files re-adds its `- id:` line. That is not a new rule,
// and reading the diff's added lines alone would say it is.
func TestMovingARuleBetweenFilesIsNotAnAddition(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	writeRules(t, root, "new.yaml", overlappingRelation)
	gitInit(t, root)
	base := commitAll(t, root, "base holds the undecidable rule already")

	if err := os.Remove(filepath.Join(root, ".formwork", "rules", "new.yaml")); err != nil {
		t.Fatal(err)
	}
	writeRules(t, root, "moved.yaml", overlappingRelation)
	commitAll(t, root, "move the rule to another file")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, verdictLine) {
		t.Fatalf("a MOVED rule was refused as newly added:\n%s", out)
	}
}

// A root that is not a git checkout is the mutation-proof scratch and the
// synthetic corpus: there is no change to judge, so the arm is inert. This is
// the stated base case, not an escape — it is unreachable from CI and from a
// developer checkout, both of which are git repos.
func TestNonGitRootLeavesTheArmInert(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	writeRules(t, root, "new.yaml", overlappingRelation)

	_, out := census(t, root)
	if strings.Contains(out, verdictLine) {
		t.Fatalf("the arm fired with no change to judge:\n%s", out)
	}
}

// In a git checkout an unresolvable range must be an error, never an empty
// added set — an empty set is a silent pass for every rule the change adds.
func TestUnresolvableRangeInAGitCheckoutFailsClosed(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	gitInit(t, root)
	commitAll(t, root, "base")
	// No env range, and no origin/develop to resolve a merge-base against.
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", "")

	code, out := census(t, root)
	if code != 2 {
		t.Fatalf("unresolvable range in a git checkout exited %d, want 2 (fail-closed):\n%s", code, out)
	}
}

// addedRuleIDs is the arm's only reader of git, and its two results carry
// different meanings that must not be conflated: an empty set in a checkout
// ("this change adds no rule") against no checkout at all ("there is no change
// to judge").
func TestAddedRuleIDsReportsCheckoutSeparatelyFromEmptiness(t *testing.T) {
	root := writeCorpus(t, baseRules, map[string]string{"src/base.go": baseFiles})
	if _, inCheckout, err := addedRuleIDs(root); err != nil || inCheckout {
		t.Fatalf("non-git root: got inCheckout=%v err=%v, want false/nil", inCheckout, err)
	}

	gitInit(t, root)
	base := commitAll(t, root, "base")
	if err := os.WriteFile(filepath.Join(root, "src", "other.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "no rule added")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	added, inCheckout, err := addedRuleIDs(root)
	if err != nil {
		t.Fatal(err)
	}
	if !inCheckout {
		t.Fatal("git checkout reported as not a checkout")
	}
	if len(added) != 0 {
		t.Fatalf("added=%v, want empty — the change adds no rule", added)
	}
}
