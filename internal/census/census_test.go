package census

import (
	"strings"
	"testing"
)

func TestReconcileAFlaggedArmIsAProblem(t *testing.T) {
	var sb strings.Builder
	n := Reconcile(&sb, "demo-rule", []Finding{{File: "f.yaml", Line: 7, Arm: "arm-one"}}, "it is wrong")
	if n != 1 {
		t.Fatalf("problems = %d, want 1", n)
	}
	if !strings.Contains(sb.String(), "NEW f.yaml:7") || !strings.Contains(sb.String(), "arm-one") || !strings.Contains(sb.String(), "it is wrong") {
		t.Fatalf("report must name the offender and why: %q", sb.String())
	}
}

// Two arms in one file are two problems: the count is per ARM, so curing one
// arm of a two-arm file still leaves the census failing on the other.
func TestReconcileCountsEveryArm(t *testing.T) {
	var sb strings.Builder
	flags := []Finding{{File: "f.yaml", Line: 3, Arm: "arm-one"}, {File: "f.yaml", Line: 9, Arm: "arm-two"}}
	if n := Reconcile(&sb, "demo-rule", flags, "it is wrong"); n != 2 {
		t.Fatalf("problems = %d, want 2: %q", n, sb.String())
	}
	if !strings.Contains(sb.String(), "2 flagged arm(s) in 1 file(s)") {
		t.Fatalf("summary must count arms and files: %q", sb.String())
	}
}

func TestReconcileNoFlagsIsTheOnlyPass(t *testing.T) {
	var sb strings.Builder
	if n := Reconcile(&sb, "demo-rule", nil, "unused"); n != 0 || !strings.Contains(sb.String(), "OK") {
		t.Fatalf("problems = %d, report %q; want 0 and OK", n, sb.String())
	}
}
