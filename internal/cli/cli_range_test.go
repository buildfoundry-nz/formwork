package cli_test

// Issue #4 — under a --staged/--range changeset scan, whole-repo-INVARIANT
// rules (required-pattern in exists mode, set-relation, pattern-count,
// baseline) must still be evaluated over the WHOLE tree, never over the
// changeset alone. They are non-monotonic under file removal: restricting the
// input to the changed files can flip a true PASS into a false "not found" /
// "subset violated" finding merely because the file bearing the required token
// is not in the range. Per-file (monotonic) rules — forbidden-pattern &c. —
// stay range-scoped for speed, and must NOT be forced whole-tree (that is the
// whole point of the fast local-hook path).
//
// These are behavioural CLI tests: they build a throwaway git repo with a
// .formwork/ config and drive the real `check` command, reproducing the exact
// pre-push false-fail rather than a unit-level proxy. Helpers gitInit/gitRun/
// mustWrite/runCLI are shared with cli_test.go.

import (
	"path/filepath"
	"testing"
)

// invariantRules defines one required-pattern(exists) rule plus one
// set-relation(subset) rule, both scoped to every .go file. Their satisfying
// tokens live in keeper.go, which the range under test does NOT include.
const invariantRules = `rules:
  - id: token-must-exist
    type: required-pattern
    scope: {include: ['**/*.go']}
    params:
      pattern: WHOLE_TREE_TOKEN
      mode: exists
  - id: pair-subset
    type: set-relation
    scope: {include: ['**/*.go']}
    params:
      a: {files: ['**/*.go'], pattern: 'PRODUCE_([A-Z]+)', group: 1}
      b: {files: ['**/*.go'], pattern: 'CONSUME_([A-Z]+)', group: 1}
      relation: subset
`

// forbiddenOnlyRules isolates the negative-gate half of the contract: no
// invariant rule is present, so these tests observe forbidden-pattern scoping
// alone (they pass both before and after the fix — invariant-preservation
// guards, not RED cases).
const forbiddenOnlyRules = `rules:
  - id: no-forbidden
    type: forbidden-pattern
    scope: {include: ['**/*.go']}
    params: {pattern: FORBIDDEN_MARKER}
`

// initInvariantRepo builds a repo whose invariant-satisfying tokens all live in
// keeper.go (committed first), then a SECOND commit that only touches other.go
// (which bears none of them). The range HEAD~1..HEAD therefore contains a Go
// file that lacks every required token — the exact shape that false-fails.
func initInvariantRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), invariantRules)
	// keeper.go bears the whole-tree token and the CONSUME side of the subset.
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n\n// WHOLE_TREE_TOKEN\n// CONSUME_FOO\n")
	// other.go bears the PRODUCE side of the subset relation but NOT the token.
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n\n// PRODUCE_FOO\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")

	// Second commit touches ONLY other.go — the changeset lacks keeper.go.
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n\n// PRODUCE_FOO\n// touched\n")
	gitRun(t, dir, "add", "other.go")
	gitRun(t, dir, "commit", "-q", "-m", "touch other")
	return dir
}

// TestInvariantRulesEvaluateWholeTreeUnderRange is the #4 regression: a
// required-pattern(exists) and a set-relation(subset) rule whose satisfying
// tokens sit in an unchanged file must PASS a --range scan, because they are
// evaluated over the whole tree. Whole-tree (no range) must pass too.
func TestInvariantRulesEvaluateWholeTreeUnderRange(t *testing.T) {
	dir := initInvariantRepo(t)

	// Whole-tree: the invariants hold, so this passes both before and after the
	// fix — the control that proves the config is well-formed.
	if code, out, errOut := runCLI(t, "check", "-C", dir); code != 0 {
		t.Fatalf("whole-tree check should pass (exit 0), got %d:\n%s%s", code, out, errOut)
	}

	// --range HEAD~1..HEAD changes only other.go, which lacks WHOLE_TREE_TOKEN
	// and CONSUME_FOO. Range-scoped evaluation of the invariants false-fails
	// (RED before the fix); whole-tree evaluation of the invariants passes.
	code, out, errOut := runCLI(t, "check", "-C", dir, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("range check should pass once invariant rules evaluate whole-tree (exit 0), got %d:\n%s%s", code, out, errOut)
	}
}

// TestStagedInvariantIgnoresUntrackedFiles pins finding #4[0]: under --staged
// (the pre-commit hook lane) a whole-tree-invariant rule must be evaluated over
// the TRACKED tree, not the whole working tree, so an untracked scratch file
// the developer is not committing cannot false-fail the commit.
func TestStagedInvariantIgnoresUntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), invariantRules)
	// keeper.go satisfies both invariants: it bears the exists token and a
	// balanced PRODUCE/CONSUME pair (subset holds: A={FOO} ⊆ B={FOO}).
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n\n// WHOLE_TREE_TOKEN\n// PRODUCE_FOO\n// CONSUME_FOO\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")

	// Stage a benign tracked change so the staged changeset is non-empty.
	mustWrite(t, filepath.Join(dir, "staged.go"), "package p\n")
	gitRun(t, dir, "add", "staged.go")

	// An UNTRACKED scratch file adds an orphan PRODUCE with no matching CONSUME.
	// It is neither staged nor committed; before the tracked-files restriction
	// it was scanned as part of the whole working tree and false-failed the
	// subset relation (RED).
	mustWrite(t, filepath.Join(dir, "scratch.go"), "package p\n\n// PRODUCE_ORPHAN\n")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged")
	if code != 0 {
		t.Fatalf("--staged invariant must ignore untracked files (exit 0), got %d:\n%s%s", code, out, errOut)
	}
}

// TestStagedInvariantEvaluatesTrackedFilesOutsideChangeset is the #4 guard for
// the staged path (finding #4[1]): an invariant's satisfying token lives in a
// tracked file that is NOT part of the staged changeset, and the invariant must
// still pass because it is evaluated over the tracked tree, not the staged
// subset. Guards against a future refactor range-scoping invariants under
// --staged.
func TestStagedInvariantEvaluatesTrackedFilesOutsideChangeset(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), invariantRules)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n\n// WHOLE_TREE_TOKEN\n// PRODUCE_FOO\n// CONSUME_FOO\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")

	// Stage only a new tracked file; keeper.go (bearing every token) stays
	// tracked but outside the staged changeset.
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n")
	gitRun(t, dir, "add", "other.go")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged")
	if code != 0 {
		t.Fatalf("--staged invariant must evaluate the tracked tree, not just staged files (exit 0), got %d:\n%s%s", code, out, errOut)
	}
}

// TestForbiddenPatternStaysRangeScopedAndBites proves the negative half of the
// contract: a forbidden-pattern in a CHANGED file is still caught under --range
// (the fast negative gate keeps biting on the changeset).
func TestForbiddenPatternStaysRangeScopedAndBites(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), forbiddenOnlyRules)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n")
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")

	// Change other.go to introduce a forbidden marker.
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n\n// FORBIDDEN_MARKER here\n")
	gitRun(t, dir, "add", "other.go")
	gitRun(t, dir, "commit", "-q", "-m", "add forbidden")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--range", "HEAD~1..HEAD")
	if code != 1 {
		t.Fatalf("forbidden-pattern in the changed file must still fail under --range (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// TestForbiddenPatternNotForcedWholeTree proves the fix does NOT over-correct:
// a forbidden-pattern in an UNCHANGED file is not reported under --range (the
// per-file gate stays range-scoped, backstopped by CI's whole-tree run). If the
// fix wrongly forced every rule whole-tree, this would false-fail.
func TestForbiddenPatternNotForcedWholeTree(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), forbiddenOnlyRules)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n\n// FORBIDDEN_MARKER lives here, unchanged\n")
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")

	// Second commit touches only other.go, which carries no forbidden marker.
	mustWrite(t, filepath.Join(dir, "other.go"), "package p\n\n// clean change\n")
	gitRun(t, dir, "add", "other.go")
	gitRun(t, dir, "commit", "-q", "-m", "touch other")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("a forbidden marker in an UNCHANGED file must not be reported under --range (exit 0), got %d:\n%s%s", code, out, errOut)
	}
}
