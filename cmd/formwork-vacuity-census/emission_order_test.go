package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// emission_order_test.go — #16031: on a FAILING run the census must name its
// offenders before it says anything else.
//
// THE DEFECT. A type:command finding carries only a head and a tail of the
// detector's output: snippet() in formwork's internal/rules/command/
// command.go keeps ~400 bytes of head plus a ~400-byte tail and elides the
// middle, and it does that BEFORE the finding is constructed — so the CI
// annotation, the job log and formwork-findings.json all carry the same clipped
// string with no richer artefact to fall back on. This census opened with a
// fixed instrument-calibration block measured at 681 bytes against that
// ~400-byte head, so the retained head was always calibration, cut mid-line,
// and the verdicts were always in the elided middle. CI could not name the
// failing rule on any possible failure. Two agents misdiagnosed red merge
// gates from it.
//
// THE INVARIANT pinned here, stated so it does not rot when a budget moves:
// the FIRST line of a failing run names an offender. Asserting position zero
// rather than "within N bytes" is deliberate — it holds for any head-first
// truncation of any size, so it survives a change to snippet()'s constants and
// needs no second copy of the parser that reads them (#16000 owns that).
//
// Calibration is not deleted, only moved. It is instrument provenance: on a
// PASSING run it is the evidence the census could have seen something, which
// is why the second arm pins that a passing run still leads with it.

// censusCombined runs the census with ONE writer for both streams, because that
// is what the engine captures — cmd.CombinedOutput() in command.go. Ordering
// BETWEEN the two streams is the whole subject here, so gating_test.go's
// census() helper cannot express it: it concatenates stdout then stderr and
// would report an order no reader ever sees.
func censusCombined(t *testing.T, root string) (int, string) {
	t.Helper()
	var both bytes.Buffer
	code := run(root, true, false, &both, &both)
	return code, both.String()
}

// vacuousCorpus is a corpus carrying one offending rule: a scope.exclude naming
// a literal path that is not in the tree. Borrowed shape from
// TestDeadExcludeGlobGates so this file pins ORDER, not classification — which
// verdict the rule draws is that file's subject, not this one's.
func vacuousCorpus(t *testing.T) string {
	t.Helper()
	return writeCorpus(t, `rules:
  - id: zz-order-probe-dead-exclude
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/nowhere/gone.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
}

// TestFailingCensusNamesOffendersFirst is the defect's direct pin: the offending
// rule id must be on line one, ahead of the calibration block.
func TestFailingCensusNamesOffendersFirst(t *testing.T) {
	code, out := censusCombined(t, vacuousCorpus(t))
	if code != 1 {
		t.Fatalf("census exited %d, want 1 — the corpus must FAIL for this arm to measure anything;\n%s", code, out)
	}
	first := strings.SplitN(strings.TrimLeft(out, "\n"), "\n", 2)[0]
	if !strings.Contains(first, "zz-order-probe-dead-exclude") {
		t.Fatalf("the first line of a failing run does not name the offending rule.\n"+
			"A CI reader sees this line and roughly 400 bytes more; everything after that is elided.\n"+
			" first line: %q\nfull output:\n%s", first, out)
	}
}

// TestFailingCensusPutsOffendersAheadOfCalibration is the same invariant stated
// as an ordering, so a roll-up that lands first but is followed by calibration
// AHEAD of the detail still fails: the detail is what a fixer acts on.
func TestFailingCensusPutsOffendersAheadOfCalibration(t *testing.T) {
	_, out := censusCombined(t, vacuousCorpus(t))
	offender := strings.Index(out, "zz-order-probe-dead-exclude")
	calibration := strings.Index(out, "instrument calibration")
	if offender < 0 || calibration < 0 {
		t.Fatalf("expected both an offender and a calibration block; offender=%d calibration=%d\n%s",
			offender, calibration, out)
	}
	if offender > calibration {
		t.Fatalf("calibration (byte %d) precedes the offender (byte %d) — the offender is in the elided middle;\n%s",
			calibration, offender, out)
	}
}

// TestPassingCensusStillLeadsWithCalibration pins the other half of the trade.
// Calibration is instrument provenance: a passing census claims every rule can
// fail, and that claim is only worth reading if the instrument demonstrated it
// could see. Moving offenders forward must not cost a passing run that evidence.
func TestPassingCensusStillLeadsWithCalibration(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: zz-order-probe-live
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	// A rule with no fixture tree draws NO-FIXTURE and the run fails, which
	// would leave this arm measuring the failing path it is here to contrast.
	writeFixture(t, root, "zz-order-probe-live", "fire-1", map[string]string{"src/x.go": "package x\n// BANNED\n"})
	writeFixture(t, root, "zz-order-probe-live", "pass-1", map[string]string{"src/x.go": "package x\n"})

	code, out := censusCombined(t, root)
	if code != 0 {
		t.Fatalf("census exited %d, want 0 — this corpus carries no offender;\n%s", code, out)
	}
	first := strings.SplitN(strings.TrimLeft(out, "\n"), "\n", 2)[0]
	if !strings.Contains(first, "instrument calibration") {
		t.Fatalf("a passing run no longer leads with its calibration evidence.\n first line: %q\n%s", first, out)
	}
}

// manyOffenderCorpus builds a corpus of n rules that each draw a verdict.
func manyOffenderCorpus(t *testing.T, n int) string {
	t.Helper()
	var rules strings.Builder
	rules.WriteString("rules:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&rules, `  - id: zz-order-probe-many-%02d
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/nowhere/gone-%02d.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, i, i)
	}
	return writeCorpus(t, rules.String(), map[string]string{"src/a.go": "package a\n"})
}

// namesInRollup counts the rule ids the roll-up line spells out.
func namesInRollup(t *testing.T, n int) (int, string) {
	t.Helper()
	code, out := censusCombined(t, manyOffenderCorpus(t, n))
	if code != 1 {
		t.Fatalf("census exited %d over %d offending rules, want 1;\n%s", code, n, out)
	}
	first := strings.SplitN(strings.TrimLeft(out, "\n"), "\n", 2)[0]
	return strings.Count(first, "zz-order-probe-many-"), out
}

// TestRollupDoesNotGrowWithTheCorpus pins the roll-up as a LINE rather than a
// listing. Stated as "the same number of names at two corpus sizes" so it needs
// no cap constant and no byte budget: whatever the cap is, it must not be the
// offender count.
//
// This is not hypothetical. The census's OWN mutation proof blanks the
// fixture-directory prefix, which makes every one of the ~2198 fixture-carrying
// rules report at once. A roll-up that named them all would be a single ~115 KB
// line, and leading with the offenders would buy a reader about seven ids and
// no diagnosis at all — on precisely the failure that rule's proof exercises.
func TestRollupDoesNotGrowWithTheCorpus(t *testing.T) {
	small, _ := namesInRollup(t, 10)
	large, out := namesInRollup(t, 30)
	if small != large {
		t.Fatalf("roll-up named %d rules at 10 offenders and %d at 30 — the line grows with the corpus;\n%s",
			small, large, out)
	}
	if !strings.Contains(out, fmt.Sprintf("and %d more", 30-large)) {
		t.Fatalf("roll-up does not say how many of the 30 it left unnamed, so the count is unrecoverable;\n%s", out)
	}
}

// TestBoundedRollupKeepsTheFirstFindingAtAFixedOffset is the reason the cap is
// worth having: names alone do not tell a fixer what went wrong. Behind a line
// that does not grow, the first COMPLETE finding — its code, its subject, its
// cure — must start at the same place whether ten rules failed or thirty.
// Stated as an equality between two corpus sizes for the same reason as the
// arm above: it needs no cap constant and no byte budget.
func TestBoundedRollupKeepsTheFirstFindingAtAFixedOffset(t *testing.T) {
	offset := func(n int) (int, string) {
		code, out := censusCombined(t, manyOffenderCorpus(t, n))
		if code != 1 {
			t.Fatalf("census exited %d over %d offending rules, want 1;\n%s", code, n, out)
		}
		i := strings.Index(out, "src/nowhere/gone-00.go")
		if i < 0 {
			t.Fatalf("the first finding's subject never appears over %d offenders;\n%s", n, out)
		}
		return i, out
	}
	small, _ := offset(10)
	large, out := offset(30)
	// Not equality: the roll-up carries the offender count, so its digit width
	// legitimately moves the offset by a byte or two. The invariant is that the
	// offset does not SCALE — fewer than one byte per additional failing rule.
	// Uncapped this grew 980 -> 2100 across the same twenty rules.
	if grew, extra := large-small, 30-10; grew >= extra {
		t.Fatalf("the first finding starts %d bytes in at 10 offenders and %d at 30 — %d bytes for %d extra "+
			"rules, so every extra failing rule pushes the diagnosis further out of reach;\n%s",
			small, large, grew, extra, out)
	}
}
