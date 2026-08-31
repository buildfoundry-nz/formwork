// RED tests for #15714 — a call anchor that can no longer see its subject.
//
// Every case here is a defect a prototype of this detector actually produced
// against the live corpus, not a hypothetical. The three false-positive shapes
// (unanchored prefix, func-typed field, line-wrapped call) each made the
// prototype report a WORKING rule as blind, which is the dangerous direction:
// it trains readers to dismiss the census.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A call split across lines is the ORDINARY spelling of a call with more than
// two arguments, and #15784 found four live violations invisible to their own
// rules for exactly this reason. go/ast sees the call whole; a line scan does
// not, and would report this live subject as dead.
func TestCallSelectorsSeesLineWrappedCall(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", `package pkg

func caller() {
	extractionruns.InsertOrAdoptActive(
		ctx,
		tx,
		projectID,
	)
}
`)
	sel, err := callSelectors(root, []string{"**/*.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sel["extractionruns.InsertOrAdoptActive"] {
		t.Fatalf("line-wrapped call not seen; selectors=%v", sel)
	}
}

func TestCallSelectorsSpellsQualifiedAndReceiverForms(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/a.go", `package pkg

func caller() {
	Bare()
	metriclock.LockSQL()
	c.readNodes()
}
`)
	sel, err := callSelectors(root, []string{"**/*.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bare", "metriclock.LockSQL", "c.readNodes"} {
		if !sel[want] {
			t.Errorf("missing selector %q; got %v", want, sel)
		}
	}
}

// A func-typed struct field is called through its receiver and has no `func`
// declaration anywhere. A declaration index misses it and calls the rule
// blind; the call plane sees it.
func TestFuncTypedFieldCallResolves(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "census/live.go", `package census

type collector struct {
	readNodes func() ([]Record, error)
}

func (c *collector) run() { c.readNodes() }
`)
	sel, err := callSelectors(root, []string{"**/*.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	live, err := anchorLive(`(^|\.)readNodes$`, sel)
	if err != nil {
		t.Fatal(err)
	}
	if !live {
		t.Fatalf("func-typed field call read as a dead anchor; selectors=%v", sel)
	}
}

// `workflowdata\.ListProjectPageRefs` is UNANCHORED in the live corpus and
// matches the real, longer `…OfTypes` selector. Normalising anchors to
// whole-identifier equality reports this working rule as blind.
func TestAnchorLiveMatchesUnanchoredPrefix(t *testing.T) {
	sel := map[string]bool{"workflowdata.ListProjectPageRefsOfTypes": true}
	live, err := anchorLive(`workflowdata\.ListProjectPageRefs`, sel)
	if err != nil {
		t.Fatal(err)
	}
	if !live {
		t.Fatal("unanchored prefix anchor read as dead against its real selector")
	}
}

func TestAnchorLiveFalseWhenNothingMatches(t *testing.T) {
	sel := map[string]bool{"metriclock.LockSQLRenamed": true}
	live, err := anchorLive(`(^|\.)LockSQL$`, sel)
	if err != nil {
		t.Fatal(err)
	}
	if live {
		t.Fatal("renamed subject still read as live")
	}
}

// The end-to-end shape: a renamed subject leaves the arm blind and it is
// flagged, keyed FILE:ARM-ID for the shrink-only debt list.
func TestDetectFlagsBlindAnchor(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/a.go", `package api

func caller() { metriclock.LockSQLRenamed() }
`)
	writeFile(t, root, ".formwork/rules/r.yaml", `rules:
- id: page-metrics-lock-sql-confined
  type: go/call-confined-to-func-name
  scope:
    include:
    - api/**/*.go
  params:
    symbol: '(^|\.)LockSQL$'
    allowed_func: '^LockPageInTx$'
`)
	flags, _, err := detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 1 {
		t.Fatalf("want 1 blind anchor, got %d: %+v", len(flags), flags)
	}
	if got := flags[0].Key(); got != ".formwork/rules/r.yaml:page-metrics-lock-sql-confined" {
		t.Errorf("debt key = %q", got)
	}
}

// The ban idiom: allowed_func admits no func name, so zero matching calls is
// the arm's compliant END STATE. Flagging it fires on a correct rule.
func TestDetectDoesNotFlagBanIdiom(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/a.go", "package api\n\nfunc caller() { other() }\n")
	writeFile(t, root, ".formwork/rules/r.yaml", `rules:
- id: count-wall-height-helper-callers-confined
  type: go/call-confined-to-func-name
  scope:
    include:
    - api/**/*.go
  params:
    symbol: '(^|\.)countWallHeightMetrics$'
    allowed_func: '^$'
`)
	flags, st, err := detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 0 {
		t.Fatalf("ban idiom flagged: %+v", flags)
	}
	if st.waived != 1 {
		t.Errorf("waived = %d, want 1", st.waived)
	}
}

// A rule whose whole subject IS the gate tree can only be witnessed by gate
// sources — the rationale formwork-vacuity-census's detectorWitnesses applies.
func TestDetectWaivesGateTreeScope(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "tools/gate/a.go", "package gate\n\nfunc caller() { other() }\n")
	writeFile(t, root, ".formwork/rules/r.yaml", `rules:
- id: gate-about-a-gate
  type: go/guard-precedes-call
  scope:
    include:
    - tools/**/*.go
  params:
    guard: '^mustCheck$'
    sink: '^neverPresent$'
`)
	flags, st, err := detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 0 {
		t.Fatalf("gate-tree-scoped arm flagged: %+v", flags)
	}
	if st.waived != 1 {
		t.Errorf("waived = %d, want 1", st.waived)
	}
}

// An arm of an ENGINE-PROBED type is not this census's business: AnchorProbe
// already emits a scope-wide verdict for it, and a second report would be
// duplicate noise on the same defect.
func TestDetectIgnoresEngineProbedTypes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/a.go", "package api\n\nfunc caller() { other() }\n")
	writeFile(t, root, ".formwork/rules/r.yaml", `rules:
- id: material-membership-prune-after-persist
  type: go/call-order-in-func
  scope:
    include:
    - api/**/*.go
  params:
    funcs: '^PersistFoldRenamed$'
    sequence:
    - insertFoldMemberships
    - PruneStaleMemberships
`)
	flags, _, err := detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 0 {
		t.Fatalf("engine-probed type flagged: %+v", flags)
	}
}

func TestMatchGlobHonoursDoubleStar(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"api/**/*.go", "api/a.go", true},
		{"api/**/*.go", "api/deep/nested/b.go", true},
		{"api/**/*.go", "other/a.go", false},
		{"**/*.go", "a/b/c.go", true},
		{"**/*_test.go", "api/a.go", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.glob, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}
