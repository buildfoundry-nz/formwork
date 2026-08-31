package census

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeList(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "debt.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadDebtListIgnoresCommentsAndBlanks(t *testing.T) {
	got, err := ReadDebtList(writeList(t, "# a reason\n\n.formwork/rules/a.yaml:arm-one\n\n   \n.formwork/rules/b.yaml:arm-two\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[".formwork/rules/a.yaml:arm-one"] || !got[".formwork/rules/b.yaml:arm-two"] {
		t.Fatalf("ReadDebtList = %v", got)
	}
}

// A missing list is an ENV error, not an empty list. Treating it as empty would
// make a deleted allowlist read as "no debt" and quietly pass every arm that
// was being carried.
func TestReadDebtListMissingFileIsAnError(t *testing.T) {
	if _, err := ReadDebtList(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Fatal("a missing debt list must be an error, not an empty set")
	}
}

func TestReconcileNewOffenderIsAProblem(t *testing.T) {
	var sb strings.Builder
	n := Reconcile(&sb, "demo-rule", []Finding{{File: "f.yaml", Line: 7, Arm: "arm-one"}}, map[string]bool{}, "it is wrong")
	if n != 1 {
		t.Fatalf("problems = %d, want 1", n)
	}
	if !strings.Contains(sb.String(), "NEW f.yaml:7") || !strings.Contains(sb.String(), "arm-one") {
		t.Fatalf("report must name the offender: %q", sb.String())
	}
}

func TestReconcileKnownDebtIsCarried(t *testing.T) {
	var sb strings.Builder
	debt := map[string]bool{"f.yaml:arm-one": true}
	n := Reconcile(&sb, "demo-rule", []Finding{{File: "f.yaml", Line: 7, Arm: "arm-one"}}, debt, "it is wrong")
	if n != 0 {
		t.Fatalf("problems = %d, want 0 — an allowlisted arm is carried", n)
	}
	if !strings.Contains(sb.String(), "known debt") {
		t.Fatalf("carried debt must still be visible: %q", sb.String())
	}
}

// Shrink-only: an entry whose arm stopped tripping must fail, so curing an arm
// forces its entry out instead of leaving a permanent waiver behind.
func TestReconcileStaleEntryIsAProblem(t *testing.T) {
	var sb strings.Builder
	debt := map[string]bool{"f.yaml:cured-arm": true}
	n := Reconcile(&sb, "demo-rule", nil, debt, "it is wrong")
	if n != 1 {
		t.Fatalf("problems = %d, want 1", n)
	}
	if !strings.Contains(sb.String(), "STALE f.yaml:cured-arm") {
		t.Fatalf("report must name the stale entry: %q", sb.String())
	}
}

// The keys are per-ARM, not per-FILE: a file-keyed list waves through a NEW
// offending arm added to any listed file, which is the hole the ratchets exist
// to close.
func TestReconcileDebtIsKeyedPerArmNotPerFile(t *testing.T) {
	var sb strings.Builder
	debt := map[string]bool{"f.yaml:arm-one": true}
	n := Reconcile(&sb, "demo-rule", []Finding{
		{File: "f.yaml", Line: 7, Arm: "arm-one"},
		{File: "f.yaml", Line: 20, Arm: "arm-two"},
	}, debt, "it is wrong")
	if n != 1 {
		t.Fatalf("problems = %d, want 1 — the sibling arm in a listed file is NOT covered", n)
	}
	if !strings.Contains(sb.String(), "arm-two") {
		t.Fatalf("report must name the uncovered sibling: %q", sb.String())
	}
}

// Deterministic output: stale entries come out sorted, not in map order, so a
// CI log diff is meaningful.
func TestReconcileStaleEntriesAreSorted(t *testing.T) {
	var sb strings.Builder
	debt := map[string]bool{"z.yaml:arm": true, "a.yaml:arm": true, "m.yaml:arm": true}
	Reconcile(&sb, "demo-rule", nil, debt, "it is wrong")
	out := sb.String()
	ai, mi, zi := strings.Index(out, "a.yaml"), strings.Index(out, "m.yaml"), strings.Index(out, "z.yaml")
	if !(ai < mi && mi < zi) {
		t.Fatalf("stale entries must be sorted: %q", out)
	}
}
