package main

import (
	"strings"
	"testing"
)

// TestIsJudgedArm pins the subject test on the shapes the census must tell
// apart. The same-dir and explicit same-file rows are load-bearing: a
// declared unit is a deliberate design act, and flagging it would demand a
// `where:` edit that fires on correct code — a rule with that false-positive
// rate gets disabled, which is worse than the count-blindness it replaced.
func TestIsJudgedArm(t *testing.T) {
	cases := []struct {
		name string
		a    arm
		want bool
	}{
		{"where omitted is same-file, judged", arm{Type: "pair-consistency"}, true},
		{"explicit same-file is a design act, not this census's question", arm{Type: "pair-consistency", Where: "same-file"}, false},
		{"same-func is per-occurrence already", arm{Type: "pair-consistency", Where: "same-func"}, false},
		{"same-dir is a design act, not this census's question", arm{Type: "pair-consistency", Where: "same-dir"}, false},
		{"countable enforces the count relation", arm{Type: "pair-consistency", Obligation: "countable"}, false},
		{"countable at file grain is per-occurrence", arm{Type: "pair-consistency", Where: "same-file", Obligation: "countable"}, false},
		{"not a pair arm", arm{Type: "required-pattern"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isJudgedArm(tc.a); got != tc.want {
				t.Errorf("isJudgedArm(%+v) = %v, want %v", tc.a, got, tc.want)
			}
		})
	}
}

// TestCountBlind pins the verdict primitive on both clauses — the count AND
// the per-file domain. Dart and proto are in domain now that same-func has
// units for them (#12195); shell stays out (no unit).
func TestCountBlind(t *testing.T) {
	cases := []struct {
		name string
		n    int
		path string
		want bool
	}{
		{"two triggers in one Go file", 2, "internal/x/writer.go", true},
		{"one trigger passes", 1, "internal/x/writer.go", false},
		{"no trigger", 0, "internal/x/writer.go", false},
		{"Dart evidence is judged now that same-func has a Dart unit", 5, "packages/x/lib/y.dart", true},
		{"proto evidence is judged now that same-func has a proto unit", 8, "schema/proto/x/y.proto", true},
		{"shell evidence is out of domain", 3, "scripts/check-x.sh", false},
		{"a _test.go file is Go", 2, "api-factory/internal/x/writer_test.go", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countBlind(tc.n, tc.path); got != tc.want {
				t.Errorf("countBlind(%d, %q) = %v, want %v", tc.n, tc.path, got, tc.want)
			}
		})
	}
}

// TestLoadCorpusReadsPairParams pins that params come from a real YAML decode
// and each arm keeps its own line — a shifted range attaches one arm's verdict
// to its neighbour.
func TestLoadCorpusReadsPairParams(t *testing.T) {
	arms, err := loadCorpus("testdata/fire-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 4 {
		t.Fatalf("arms: %d, want 4", len(arms))
	}
	if arms[0].ID != "demo-count-blind" || arms[0].Where != "" || arms[0].Obligation != "" {
		t.Errorf("arm 0: %+v", arms[0])
	}
	if arms[1].Where != "same-func" || arms[2].Obligation != "countable" || arms[3].Where != "same-dir" {
		t.Errorf("params not decoded per arm: %+v", arms)
	}
	for i := 1; i < len(arms); i++ {
		if arms[i-1].Line >= arms[i].Line {
			t.Errorf("arm lines not in declaration order: %d >= %d", arms[i-1].Line, arms[i].Line)
		}
	}
	if arms[0].File != ".formwork/rules/demo.yaml" {
		t.Errorf("file: %q", arms[0].File)
	}
}

// TestDetectFlagsTheFileGrainCountBlindArm is the fire case: of the four arms
// over the same corpus, only the omitted-where presence arm is judged, and
// both locks.go (2 trigger lines) and locks.dart (3 lines) offend — extra.go
// (1 line) passes. Dart is in domain now that same-func has a Dart unit
// (#12195).
func TestDetectFlagsTheFileGrainCountBlindArm(t *testing.T) {
	root := "testdata/fire-1"
	arms, err := loadCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	bad, examined, err := detect(root, arms)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 1 {
		t.Errorf("examined %d, want 1 — only the omitted-where presence arm is judged", examined)
	}
	if len(bad) != 2 {
		t.Fatalf("flagged: %+v", bad)
	}
	var sawGo, sawDart bool
	for _, b := range bad {
		if b.rule != "demo-count-blind" {
			t.Errorf("unexpected arm %q", b.rule)
		}
		if strings.Contains(b.why, "src/locks.go") && strings.Contains(b.why, "2 trigger lines") {
			sawGo = true
		}
		if strings.Contains(b.why, "src/locks.dart") && strings.Contains(b.why, "3 trigger lines") {
			sawDart = true
		}
	}
	if !sawGo || !sawDart {
		t.Errorf("detail should name both evidence files: %+v", bad)
	}
}

// TestDetectPassesACleanCorpus is the control that makes the fire mean
// something: one trigger per file, and a Dart-only multi-trigger arm, must
// never be flagged — a false positive here retires live gates.
func TestDetectPassesACleanCorpus(t *testing.T) {
	root := "testdata/pass-1"
	arms, err := loadCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	bad, examined, err := detect(root, arms)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 1 {
		t.Errorf("examined %d, want 1 — the Dart arm declares where: same-file and is not judged", examined)
	}
	if len(bad) != 0 {
		t.Fatalf("flagged: %+v", bad)
	}
}

// TestRunExitCodes pins the contract the formwork `command` rule reads:
// 0 clean, 1 offenders, 2 usage/env error.
func TestRunExitCodes(t *testing.T) {
	var out, errOut strings.Builder
	if code := run("testdata/fire-1", &out, &errOut); code != 1 {
		t.Errorf("offending corpus exit = %d, want 1\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "pair-consistency-not-for-countable-obligations: 2 offending arm(s)") {
		t.Errorf("the report must name the rule and the offender count:\n%s", out.String())
	}
	out.Reset()
	if code := run("testdata/pass-1", &out, &errOut); code != 0 {
		t.Errorf("clean corpus exit = %d, want 0\n%s", code, out.String())
	}
	out.Reset()
	if code := run("testdata/does-not-exist", &out, &errOut); code != 2 {
		t.Errorf("unreadable root exit = %d, want 2", code)
	}
}

// TestRunRefusesAVacuousPass is the detector's self-check: a run that read NO
// rule file is a broken invocation wearing a pass — a wrong root, a moved
// rules directory — and exiting 2 rather than 0 is what keeps it
// distinguishable from a clean corpus. An empty SUBJECT (rule files, no
// judged arms) is a legitimate pass and is what the mutation-proof scratch
// hands this census.
func TestRunRefusesAVacuousPass(t *testing.T) {
	var out, errOut strings.Builder
	if code := run("testdata/empty-1", &out, &errOut); code != 2 {
		t.Fatalf("a run that read zero rule files must refuse to report a pass; exit=%d\n%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "refusing to report a pass") {
		t.Fatalf("the refusal must say WHY, not just exit non-zero.\n%s", errOut.String())
	}
}
