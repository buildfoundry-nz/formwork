package cli_test

import (
	"reflect"
	"testing"
)

// A scope floor is a claim about the tree, which ScopeFloorFindings checks
// outside engine.Run. The rule's per-file verdict on one content is the same
// as in a whole-tree check, so the floor must not hide it.
func TestCheckContentEvaluatesARuleWithAScopeFloor(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: floored
    type: forbidden-pattern
    scope: {include: ['**'], min_files: 50}
    params: {pattern: TODO}
  - id: floored-tree
    type: required-pattern
    scope: {include: ['**'], min_files: 50}
    params: {pattern: REQUIRED, mode: exists}
`)
	code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", "a.go")
	if code != 1 || !reflect.DeepEqual(doc.EvaluatedRuleIDs, []string{"floored"}) {
		t.Fatalf("floored rule: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	if len(doc.Findings) != 1 || doc.Findings[0].Rule != "floored" || doc.Findings[0].Line != 1 {
		t.Fatalf("want the pattern finding alone, never a floor finding: %+v", doc.Findings)
	}
	if got := notEvaluatedReasons(doc); got["floored-tree"] != "finalizer" {
		t.Fatalf("a floor must not excuse a tree-dependent checker: %v", got)
	}
	code, clean, errOut := runContent(t, "fine\n", "check-content", "-C", root, "--path", "a.go")
	if code != 0 || clean.FindingsTotal != 0 || clean.Status != "clean" {
		t.Fatalf("clean content under a floor of 50 must pass: code=%d doc=%+v stderr=%s", code, clean, errOut)
	}
}
