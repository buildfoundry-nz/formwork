package main

import (
	"strings"
	"testing"
)

// I17: DEAD-TRIGGER must treat also_present as part of the obligation entry
// condition. A live trigger with a dead also_present never enters the requires
// half — the pair-consistency spelling of DEAD-TRIGGER incomplete for the arm
// the same wave introduced.
func TestDeadAlsoPresentGatesAsDeadTrigger(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-also
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      where: same-func
      trigger: 'projectMetricUnionSQL'
      also_present: 'tx\.(Query|QueryRow)\('
      requires: 'projectreadscope\.Memo\('
    tags: [always]
`, map[string]string{
		// Trigger lives; also_present (tx.Query) never appears → obligation never enters.
		"src/a.go": "package a\n\nfunc Load() {\n\t_ = projectMetricUnionSQL\n\t_ = projectreadscope.Memo\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-TRIGGER") {
		t.Fatalf("live trigger + dead also_present must gate DEAD-TRIGGER:\n%s", out)
	}
}

// I18: same-func DEAD-TRIGGER must MatchString on function spans, not per-line.
// A multi-line-only trigger that only matches inside a func span is LIVE.
func TestSameFuncMultiLineTriggerIsNotDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: multiline-live
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      where: same-func
      trigger: 'alpha[\s\S]*?beta'
      requires: 'Memo\('
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc Load() {\n\talpha\n\tbeta\n\tMemo()\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-TRIGGER") {
		t.Fatalf("multi-line same-func trigger must not classify DEAD-TRIGGER:\n%s", out)
	}
}

// I18 converse: trigger noise only outside any function span is dead under
// where: same-func — the engine never enters an obligation on package-level residue.
func TestSameFuncOutOfFuncTriggerIsDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: out-of-func-dead
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      where: same-func
      trigger: 'projectMetricUnionSQL'
      requires: 'Memo\('
    tags: [always]
`, map[string]string{
		// Trigger only at package level — same-func residue, not a unit.
		"src/a.go": "package a\n\nconst projectMetricUnionSQL = \"x\"\n\nfunc Load() {\n\tMemo()\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-TRIGGER") {
		t.Fatalf("out-of-func trigger residue under same-func must gate DEAD-TRIGGER:\n%s", out)
	}
}

// #12195: same-func Dart/proto units live in the engine. Vacuity cannot parse
// them (obligationLiveSameFunc is Go-only), so a live Dart trigger must not
// be classified DEAD-TRIGGER just because this census cannot walk Dart
// braces. Per-line appearance is the conservative liveness check.
func TestSameFuncDartTriggerIsNotDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dart-same-func-live
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.dart']
    params:
      where: same-func
      trigger: 'nextPageToken'
      requires: 'page_token'
    tags: [always]
`, map[string]string{
		"src/a.dart": "void list() {\n  final t = nextPageToken;\n  send(page_token: t);\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-TRIGGER") {
		t.Fatalf("a Dart same-func arm whose trigger appears in source must not be DEAD-TRIGGER:\n%s", out)
	}
}

// V4 EMPTY-SIDE: equal|subset with zero extracted elements on a live side is
// a vacuous join (empty=empty / empty⊆B). Gate it so the census cannot OK a
// name-only relation that extracts nothing on the real tree.
func TestEmptySideGatesEqualRelation(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: empty-side-equal
    type: set-relation
    severity: error
    scope:
      include: ['src/**']
    params:
      a: { files: ['src/left/**'], pattern: 'tag:([a-z]+)', group: 1 }
      b: { files: ['src/right/**'], pattern: 'tag:([a-z]+)', group: 1 }
      relation: equal
    tags: [always]
`, map[string]string{
		// Files exist but patterns match nothing → empty=empty green join.
		"src/left/a.go":  "package a\n// no tags here\n",
		"src/right/b.go": "package b\n// no tags here\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "EMPTY-SIDE") {
		t.Fatalf("empty=empty equal relation must gate EMPTY-SIDE:\n%s", out)
	}
}
