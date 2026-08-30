package dartscan_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// dart/method-delegates anchors on a method NAME: it walks for a line matching
// `method:`, then asserts that method's body calls `must_call`. When no line
// matches the anchor it emits nothing — fail-OPEN, so renaming the method
// retires the delegation invariant silently. The verdict below makes an empty
// anchor set a finding instead, scope-wide (in Finalize), so a sibling file
// with no such method does not indict the rule.

const delegatingSrc = `class C {
  void flushProjectCaches() {
    _lru.drain();
  }
}
`

func finalizeDart(t *testing.T, c rules.Checker) []rules.Match {
	t.Helper()
	fin, ok := c.(rules.Finalizer)
	if !ok {
		t.Fatalf("%T does not implement rules.Finalizer — the anchor verdict has nowhere to land", c)
	}
	return fin.Finalize()
}

func TestMethodDelegatesAnchorAbsentIsFinding(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", "{method: 'void flushRenamed\\(', must_call: '_lru\\.drain\\('}")
	ms, err := check(t, c, file("a.dart", delegatingSrc))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("per-file pass expected, got %+v", ms)
	}
	fms := finalizeDart(t, c)
	if len(fms) != 1 {
		t.Fatalf("want 1 missing-anchor finding, got %+v", fms)
	}
	if !strings.Contains(fms[0].Message, "flushRenamed") {
		t.Errorf("finding must name the dead anchor, got %q", fms[0].Message)
	}
	if !rules.IsWholeTreeInvariant(c) {
		t.Errorf("%T is not a whole-tree invariant: under --staged the file declaring the method may be outside the changeset", c)
	}
}

func TestMethodDelegatesAnchorPresentIsSilent(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", "{method: 'void flushProjectCaches\\(', must_call: '_lru\\.drain\\('}")
	if _, err := check(t, c, file("a.dart", delegatingSrc)); err != nil {
		t.Fatal(err)
	}
	if fms := finalizeDart(t, c); len(fms) != 0 {
		t.Fatalf("anchor matched a method; want no finding, got %+v", fms)
	}
}

func TestMethodDelegatesAnchorSatisfiedByAnotherFileInScope(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", "{method: 'void flushProjectCaches\\(', must_call: '_lru\\.drain\\('}")
	if _, err := check(t, c, file("other.dart", "class D {\n  void unrelated() {}\n}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := check(t, c, file("a.dart", delegatingSrc)); err != nil {
		t.Fatal(err)
	}
	if fms := finalizeDart(t, c); len(fms) != 0 {
		t.Fatalf("anchor exists elsewhere in scope; want no finding, got %+v", fms)
	}
}

// A declaration the checker deliberately skips (abstract / arrow-bodied) is
// not a body it can assert over, so it must not satisfy the anchor either —
// otherwise `void f();` in an interface would keep the invariant "alive" while
// the real implementation had been renamed away.
func TestMethodDelegatesAbstractDeclarationDoesNotSatisfyAnchor(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", "{method: 'void flushProjectCaches\\(', must_call: '_lru\\.drain\\('}")
	if _, err := check(t, c, file("iface.dart", "abstract class I {\n  void flushProjectCaches();\n}\n")); err != nil {
		t.Fatal(err)
	}
	if fms := finalizeDart(t, c); len(fms) != 1 {
		t.Fatalf("an abstract declaration has no body to check; want a missing-anchor finding, got %+v", fms)
	}
}

func TestMethodDelegatesEmptyScopeIsNotAMissingAnchor(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", "{method: 'void flushProjectCaches\\(', must_call: '_lru\\.drain\\('}")
	if fms := finalizeDart(t, c); len(fms) != 0 {
		t.Fatalf("empty scope is lint's finding, not this rule's: %+v", fms)
	}
}

// A non-Dart file in scope is not evidence about a Dart method, so it must not
// count as "seen" and trigger a missing-anchor verdict on its own.
func TestMethodDelegatesNonDartFileDoesNotArmTheVerdict(t *testing.T) {
	c := mustChecker(t, "dart/method-delegates", "{method: 'void flushProjectCaches\\(', must_call: '_lru\\.drain\\('}")
	if _, err := check(t, c, file("a.go", "package p\n")); err != nil {
		t.Fatal(err)
	}
	if fms := finalizeDart(t, c); len(fms) != 0 {
		t.Fatalf("only non-Dart files were seen; want no finding, got %+v", fms)
	}
}
