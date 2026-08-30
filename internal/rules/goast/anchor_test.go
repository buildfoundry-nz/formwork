package goast_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// The fail-open class these tests pin: a name-anchored analyzer whose anchor
// matches nothing emits no findings, so an ordinary rename retires the
// invariant with no signal. An empty anchor set must be indistinguishable from
// a violation, not from compliance.
//
// The assertion is SCOPE-WIDE, not per-file: it lands in Finalize once every
// in-scope file has been seen. A rule scoped to a package where only one file
// declares the anchored func is compliant, and only a scope where NO file
// declares it is a finding.

func finalize(t *testing.T, c rules.Checker) []rules.Match {
	t.Helper()
	fin, ok := c.(rules.Finalizer)
	if !ok {
		t.Fatalf("%T does not implement rules.Finalizer — the anchor verdict has nowhere to land", c)
	}
	return fin.Finalize()
}

func wantWholeTree(t *testing.T, c rules.Checker) {
	t.Helper()
	if !rules.IsWholeTreeInvariant(c) {
		t.Errorf("%T is not a whole-tree invariant: under --staged the file declaring the anchor may be outside the changeset, which would report a false missing-anchor", c)
	}
}

const twoFuncSrc = `package p

func Render(dpi int) int {
	clamp(dpi)
	scale(dpi)
	return dpi
}

func other() { helper() }
`

// ---- go/per-func-count-relation: funcs anchor ----

func TestCountRelationFuncsAnchorAbsentIsFinding(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{funcs: '^RenderRenamed$', left: 'clamp', right: 'scale', relation: '<='}")
	if ms := check(t, c, "a.go", twoFuncSrc); len(ms) != 0 {
		t.Fatalf("per-file pass expected, got %+v", ms)
	}
	ms := finalize(t, c)
	if len(ms) != 1 {
		t.Fatalf("want 1 missing-anchor finding, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "^RenderRenamed$") {
		t.Errorf("finding must name the dead anchor, got %q", ms[0].Message)
	}
	wantWholeTree(t, c)
}

func TestCountRelationFuncsAnchorPresentIsSilent(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{funcs: '^Render$', left: 'clamp', right: 'scale', relation: '<='}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("anchor matched a func; want no finding, got %+v", ms)
	}
}

// The anchor need only exist SOMEWHERE in scope: a sibling file with no
// matching func must not indict the rule.
func TestCountRelationFuncsAnchorSatisfiedByAnotherFileInScope(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{funcs: '^Render$', left: 'clamp', right: 'scale', relation: '<='}")
	check(t, c, "empty.go", "package p\n\nfunc unrelated() {}\n")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("anchor exists elsewhere in scope; want no finding, got %+v", ms)
	}
}

// An unanchored arm (no funcs:) applies to every func, so there is no name to
// go stale — it must not acquire a spurious finding.
func TestCountRelationWithoutFuncsAnchorHasNoAnchorVerdict(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{left: 'clamp', right: 'scale', relation: '<='}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("no funcs anchor to assert; want no finding, got %+v", ms)
	}
}

// A scope that matched zero files is empty-scope rot, which `formwork lint`
// owns — this rule must not double-report it.
func TestCountRelationEmptyScopeIsNotAMissingAnchor(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{funcs: '^Render$', left: 'clamp', right: 'scale', relation: '<='}")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("empty scope is lint's finding, not this rule's: %+v", ms)
	}
}

// ---- go/per-func-count-relation: require_symbol ----

// The unanchored arms cannot be cured by a funcs anchor they do not have: for
// them the rename target is the SYMBOL, and `count(left) <= count(right)` holds
// vacuously at 0 <= 0. require_symbol is explicit because absence is sometimes
// the compliant state (the forbidden-call-in-func idiom pins `funcs` instead
// and asserts left is absent).
func TestCountRelationRequireSymbolLeftAbsentIsFinding(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{left: 'mintKeyRenamed', right: 'scale', relation: '<=', require_symbol: left}")
	check(t, c, "a.go", twoFuncSrc)
	ms := finalize(t, c)
	if len(ms) != 1 {
		t.Fatalf("want 1 missing-symbol finding, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "mintKeyRenamed") {
		t.Errorf("finding must name the dead symbol, got %q", ms[0].Message)
	}
	wantWholeTree(t, c)
}

func TestCountRelationRequireSymbolLeftPresentIsSilent(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{left: 'clamp', right: 'scale', relation: '<=', require_symbol: left}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("left symbol present; want no finding, got %+v", ms)
	}
}

func TestCountRelationRequireSymbolRightAbsentIsFinding(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{left: 'clamp', right: 'scaleRenamed', relation: '>=', require_symbol: right}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 1 {
		t.Fatalf("want 1 missing-symbol finding, got %+v", ms)
	}
}

func TestCountRelationRequireSymbolBothReportsEachDeadSide(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{left: 'clampRenamed', right: 'scaleRenamed', relation: '==', require_symbol: both}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 2 {
		t.Fatalf("want a finding per dead side, got %+v", ms)
	}
}

func TestCountRelationRequireSymbolRejectsUnknownValue(t *testing.T) {
	err := buildErr(t, "go/per-func-count-relation", "{left: 'a', right: 'b', relation: '<=', require_symbol: middle}")
	if err == nil || !strings.Contains(err.Error(), "require_symbol") {
		t.Fatalf("unknown require_symbol must be a config error, got %v", err)
	}
}

// The funcs anchor and require_symbol are independent assertions: an arm may
// carry both, and each dead half reports separately.
func TestCountRelationFuncsAnchorAndRequireSymbolAreIndependent(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "{funcs: '^Gone$', left: 'alsoGone', right: 'scale', relation: '<=', require_symbol: left}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 2 {
		t.Fatalf("want one finding per dead anchor, got %+v", ms)
	}
}

// ---- go/call-order-in-func ----

func TestCallOrderFuncsAnchorAbsentIsFinding(t *testing.T) {
	c := build(t, "go/call-order-in-func", "{funcs: '^RenderRenamed$', sequence: ['clamp', 'scale']}")
	if ms := check(t, c, "a.go", twoFuncSrc); len(ms) != 0 {
		t.Fatalf("per-file pass expected, got %+v", ms)
	}
	ms := finalize(t, c)
	if len(ms) == 0 {
		t.Fatalf("want a missing-anchor finding")
	}
	if !strings.Contains(ms[0].Message, "^RenderRenamed$") {
		t.Errorf("finding must name the dead anchor, got %q", ms[0].Message)
	}
	wantWholeTree(t, c)
}

func TestCallOrderFuncsAnchorPresentIsSilent(t *testing.T) {
	c := build(t, "go/call-order-in-func", "{funcs: '^Render$', sequence: ['clamp', 'scale']}")
	check(t, c, "a.go", twoFuncSrc)
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("anchor and both stages present; want no finding, got %+v", ms)
	}
}

// A sequence stage that matches nothing imposes no ordering at all — and
// unlike a count relation there is no reading in which its absence is the
// compliant state, so the assertion is default-on with no param.
func TestCallOrderDeadSequenceStageIsFinding(t *testing.T) {
	c := build(t, "go/call-order-in-func", "{funcs: '^Render$', sequence: ['clamp', 'scaleRenamed']}")
	check(t, c, "a.go", twoFuncSrc)
	ms := finalize(t, c)
	if len(ms) != 1 {
		t.Fatalf("want 1 dead-stage finding, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "scaleRenamed") {
		t.Errorf("finding must name the dead stage, got %q", ms[0].Message)
	}
}

func TestCallOrderEmptyScopeIsNotAMissingAnchor(t *testing.T) {
	c := build(t, "go/call-order-in-func", "{funcs: '^Render$', sequence: ['clamp', 'scale']}")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("empty scope is lint's finding, not this rule's: %+v", ms)
	}
}
