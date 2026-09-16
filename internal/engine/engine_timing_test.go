package engine_test

import (
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestRunTimedCollectsPhase1Durations pins the accounting half of the feature:
// every rule that ran has a Timing entry, and the entry is the SUM of its
// per-file evaluations (phase 1) — a rule over three files carries one
// duration, not three.
func TestRunTimedCollectsPhase1Durations(t *testing.T) {
	hit := &fakeChecker{match: func(f *scan.File) []rules.Match {
		return []rules.Match{{Line: 1, Message: "hit"}}
	}}
	r := mustRule(t, "hit-go-files", finding.SeverityError, []string{"**/*.go"}, hit)
	fset := memFileSet(map[string]string{"a.go": "x", "b.go": "x", "c.go": "x"})

	_, timing, err := engine.RunTimed([]*config.Rule{r}, fset, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := timing["hit-go-files"]
	if !ok {
		t.Fatalf("Timing must cover every ran rule: %v", timing)
	}
	if d < 0 {
		t.Fatalf("durations cannot be negative: %v", d)
	}
	if len(timing) != 1 {
		t.Fatalf("one rule ran, one entry expected: %v", timing)
	}
}

// TestRunTimedProgressFiresPerFinalizer pins the liveness half: the callback
// is invoked once per finalizer rule with the rule's ID — what
// `check -progress` renders a line from — and Timing covers the finalizers
// alongside any phase-1 rules.
func TestRunTimedProgressFiresPerFinalizer(t *testing.T) {
	fin1 := &fakeFinalizer{}
	fin1.final = []rules.Match{{Message: "m1"}}
	fin2 := &fakeFinalizer{}
	fin2.final = []rules.Match{{Message: "m2"}}
	rls := []*config.Rule{
		mustRule(t, "fin-one", finding.SeverityError, []string{"**"}, fin1),
		mustRule(t, "fin-two", finding.SeverityError, []string{"**"}, fin2),
	}
	fset := memFileSet(map[string]string{"a.txt": "x"})

	var fired []string
	_, timing, err := engine.RunTimed(rls, fset, 2, func(ruleID string, elapsed time.Duration) {
		if elapsed < 0 {
			t.Errorf("elapsed cannot be negative: %v", elapsed)
		}
		fired = append(fired, ruleID)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fired) != 2 {
		t.Fatalf("progress must fire once per finalizer, got %v", fired)
	}
	seen := map[string]bool{}
	for _, id := range fired {
		seen[id] = true
	}
	for _, want := range []string{"fin-one", "fin-two"} {
		if !seen[want] {
			t.Fatalf("finalizer %s never reported progress: %v", want, fired)
		}
		if _, ok := timing[want]; !ok {
			t.Fatalf("Timing must cover finalizer %s: %v", want, timing)
		}
	}
}

// TestRunWrapperMatchesRunTimedFindings pins the wrapper contract: Run is
// RunTimed with no callback and discarded timing, so every existing caller
// sees identical findings and is not forced onto the new signature.
func TestRunWrapperMatchesRunTimedFindings(t *testing.T) {
	mk := func() *config.Rule {
		hit := &fakeChecker{match: func(f *scan.File) []rules.Match {
			return []rules.Match{{Line: 1, Message: "hit"}}
		}}
		return mustRule(t, "hit-go-files", finding.SeverityError, []string{"**/*.go"}, hit)
	}
	fset := memFileSet(map[string]string{"a.go": "x", "z.go": "x"})

	viaRun, err := engine.Run([]*config.Rule{mk()}, fset, 2)
	if err != nil {
		t.Fatal(err)
	}
	viaTimed, _, err := engine.RunTimed([]*config.Rule{mk()}, fset, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(viaRun) != len(viaTimed) {
		t.Fatalf("wrapper changed findings: run=%v timed=%v", viaRun, viaTimed)
	}
	for i := range viaRun {
		if viaRun[i] != viaTimed[i] {
			t.Fatalf("finding %d differs: %v vs %v", i, viaRun[i], viaTimed[i])
		}
	}
}
