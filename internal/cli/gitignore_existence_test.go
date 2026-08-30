package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// #178 — when scan.gitignore is declared and git cannot answer, nothing is
// pruned and the tree scanned is a SUPERSET of the declared one.
//
// That was argued fail-closed, and for most rules it is: a rule that fires on
// the PRESENCE of something can only gain matches from a superset, never lose
// one. The argument INVERTS for a rule that fires on an ABSENCE. A superset can
// supply the very thing whose absence was the violation, and the finding
// disappears — over a scan the operator was told was larger, not smaller.
//
// `check` already refused this for scope.min_files, because a floor is a claim
// about the declared corpus that an unpruned tree can satisfy. The same
// reasoning covers every rule whose verdict depends on the whole scanned set,
// and IsWholeTreeInvariant is exactly that population — which is why the fix is
// to widen the existing refusal rather than to enumerate rule types that would
// then need maintaining.

// existenceRepo: gitignore declared, deliberately NOT a git repository, and a
// rule that fires on an ABSENCE tree-wide.
func existenceRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n")
	// required-pattern in exists-mode is a whole-tree invariant: it reports when
	// NOTHING in scope carries the anchor, so a superset scan can supply it.
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: needs-anchor\n    type: required-pattern\n"+
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: ANCHOR, mode: exists}\n")
	mustWrite(t, filepath.Join(root, "a.txt"), "nothing here\n")
	return root
}

func TestCheckRefusesAWholeTreeInvariantOverAnUnprunedSuperset(t *testing.T) {
	root := existenceRepo(t)
	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — an absence-asserting rule cannot be judged over a "+
			"scan whose pruned set is unknown\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "could not determine") {
		t.Fatalf("stderr must name the unanswered question:\n%s", errOut)
	}
}

// The narrowing, and the reason this is not just "refuse whenever git fails".
// A presence-asserting rule is genuinely safe over a superset: it can only gain
// matches. Refusing there would turn an opt-in feature into a hard failure for
// every consumer whose tree is not a repository — an exported tarball, a corpus
// checked out standalone — and that is how a fail-closed direction gets reverted
// wholesale rather than tightened.
func TestCheckStillScansAPresenceRuleOverAnUnprunedSuperset(t *testing.T) {
	root := gitignoreRepo(t, true) // declared, not a repo
	mustWrite(t, filepath.Join(root, "noise.txt"), "banana\n")

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 — a presence rule only gains matches from a "+
			"superset, so it is still judgeable\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "nothing pruned") {
		t.Fatalf("the disclosure must survive:\n%s", errOut)
	}
}
