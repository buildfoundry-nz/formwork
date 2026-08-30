// phase1_error_order_test.go — which engine error phase 1 reports must not
// depend on who won a race.
//
// Phase 2 already holds this contract and states why: "finErrIdx keeps the
// reported error deterministic: whichever worker fails first, the error
// surfaced is the one from the earliest rule in declaration order, exactly as
// the serial pass reported it." It is pinned twice — inside one pool
// (TestRunFinalizerErrorIsDeterministic) and across the cost partition
// (TestRunFinalizerErrorIsDeterministicAcrossPools), because a regression that
// gave each pool its own error variable would slip past the first.
//
// Phase 1 held neither, and kept `firstErr` on a plain first-writer-wins: the
// error a run reports is whichever goroutine reached the mutex first. That was
// already loose across files; splitting phase 1 into two pools (#315) widened
// it to a SINGLE-file corpus, where the flat pool used to evaluate every rule
// on one goroutine in declaration order and so could not disagree with itself.
//
// An engine error aborts the run and discards every finding
// (TestRunDiscardsFindingsOnError), so the error text IS the run's entire
// output. Reporting a different rule's crash from one run to the next is a
// flake in the one message an operator has to debug from.
//
// The order pinned here is the order a --workers 1 run visits work in: files in
// scan order, and within a file rules in declaration order. Three tests, and
// each one exists because a different reduction survived the others:
//
//	rule index alone (phase 2's mechanism)   caught by ...IsTheOneTheSerialPassReports
//	newest error always wins                 caught by ...KeepsTheEarlierVisitAgainstALaterFailure
//	first error always wins (the old code)   caught by the other two
//
// The middle one is the one to keep in mind when editing these: the first two
// tests both put the CORRECT answer last in wall time, which is what makes them
// fail against arrival order and also what makes them blind to a rank that just
// overwrites. Any case added here should say which side of that it sits on.
package engine_test

import (
	"strings"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// slowErrChecker fails from CheckFile after a delay. The delay is what makes
// these tests deterministic rather than lucky: it guarantees the peer's error
// has ALREADY landed by the time this one does, so a run that still reports
// this rule can only have done so by consulting order rather than arrival.
type slowErrChecker struct{ delay time.Duration }

func (c *slowErrChecker) CheckFile(*scan.File) ([]rules.Match, error) {
	time.Sleep(c.delay)
	return nil, errFake
}

// boundSlowErrChecker is a slowErrChecker dispatched through the BOUNDED half
// of the phase-1 partition, so the two failing rules below are in different
// pools and no single goroutine ever sees both.
type boundSlowErrChecker struct{ slowErrChecker }

func (*boundSlowErrChecker) SelfBounded() bool { return true }

// fastErrChecker fails from CheckFile immediately, so it wins every wall-clock
// race against the slow one above.
type fastErrChecker struct{}

func (*fastErrChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, errFake }

// TestRunPhase1ErrorIsDeterministicAcrossPools is the direct analogue of
// TestRunFinalizerErrorIsDeterministicAcrossPools, for the partition #315 added
// to phase 1.
//
// The first-declared rule runs in the bounded half and fails LATER in wall time
// than its full-width peer. Declaration order must still decide. Before the
// partition a flat pool evaluated both rules for a one-file corpus on the same
// goroutine, in declaration order, so this could not fail; with two pools the
// two errors race, and first-writer-wins reports the second-declared rule.
func TestRunPhase1ErrorIsDeterministicAcrossPools(t *testing.T) {
	fset := memFileSet(map[string]string{"a.txt": "x"})
	for i := range 25 {
		rls := []*config.Rule{
			mustRule(t, "bound-first-failing", finding.SeverityError, []string{"**"},
				&boundSlowErrChecker{slowErrChecker{delay: 10 * time.Millisecond}}),
			mustRule(t, "wide-late-failing", finding.SeverityError, []string{"**"},
				&fastErrChecker{}),
		}
		_, err := engine.Run(rls, fset, 4)
		if err == nil {
			t.Fatalf("run %d: expected an engine error", i)
		}
		if !strings.Contains(err.Error(), "bound-first-failing") {
			t.Fatalf("run %d: error %q must name the first rule in declaration order, whichever "+
				"half of the phase-1 partition ran it and whenever its error landed — an engine "+
				"error aborts the run and is its whole output, so first-writer-wins makes that "+
				"output a coin flip (#315)", i, err)
		}
	}
}

// TestRunPhase1ErrorIsTheOneTheSerialPassReports pins the OTHER dimension of
// the key, and it is the reason the fix cannot be phase 2's verbatim: phase 2
// finalizers have no files, so a rule index orders them completely. Phase 1
// visits (file, rule) pairs, and file position dominates — a --workers 1 run
// finishes every rule on the first file before it looks at the second.
//
// Here the FIRST-declared rule is scoped to the SECOND file and fails
// instantly, while the second-declared rule is scoped to the FIRST file and
// fails slowly. A serial run reports the second-declared rule, because it never
// reaches the second file. So does a correct concurrent run.
//
// This case fails both against first-writer-wins (the fast rule's error lands
// first) AND against a fix that ranks by declaration index alone (that rule is
// also declared first) — which is what stops the pin from being satisfied by
// copying phase 2's mechanism without noticing phase 1 has one more dimension.
func TestRunPhase1ErrorIsTheOneTheSerialPassReports(t *testing.T) {
	// orderedFileSet, not memFileSet: this test is ABOUT scan order, and map
	// iteration would shuffle the two files from run to run.
	fset := orderedFileSet([]string{"f0.txt", "f1.txt"})
	for i := range 25 {
		rls := []*config.Rule{
			mustRule(t, "declared-first-on-later-file", finding.SeverityError,
				[]string{"f1.txt"}, &fastErrChecker{}),
			mustRule(t, "declared-second-on-first-file", finding.SeverityError,
				[]string{"f0.txt"}, &slowErrChecker{delay: 10 * time.Millisecond}),
		}
		_, err := engine.Run(rls, fset, 4)
		if err == nil {
			t.Fatalf("run %d: expected an engine error", i)
		}
		if !strings.Contains(err.Error(), "declared-second-on-first-file") {
			t.Fatalf("run %d: error %q must be the one a --workers 1 run reports — files in scan "+
				"order first, rules in declaration order within a file. Ranking by rule index "+
				"alone names the rule on the LATER file, which no serial run would ever have "+
				"reached (#315)", i, err)
		}
	}
}

// TestRunPhase1ErrorKeepsTheEarlierVisitAgainstALaterFailure covers the
// direction the two tests above are blind to, and it was added because a
// mutation survived them: replacing the whole ranking with "the newer error
// always wins" left both of them green.
//
// They are blind because both put the CORRECT answer LAST in wall time — that
// delay is exactly what makes them fail against arrival order — so a reduction
// that unconditionally overwrites gets both right for the wrong reason, while
// being no more deterministic than the first-writer-wins it replaced.
//
// Here the first-declared rule fails INSTANTLY and its peer in the other half
// of the partition fails 10ms later, so the answer the serial pass gives is
// also the one that arrives first. Only a rank that actually compares positions
// keeps it.
func TestRunPhase1ErrorKeepsTheEarlierVisitAgainstALaterFailure(t *testing.T) {
	fset := memFileSet(map[string]string{"a.txt": "x"})
	for i := range 25 {
		rls := []*config.Rule{
			mustRule(t, "wide-first-failing", finding.SeverityError, []string{"**"},
				&fastErrChecker{}),
			mustRule(t, "bound-late-failing", finding.SeverityError, []string{"**"},
				&boundSlowErrChecker{slowErrChecker{delay: 10 * time.Millisecond}}),
		}
		_, err := engine.Run(rls, fset, 4)
		if err == nil {
			t.Fatalf("run %d: expected an engine error", i)
		}
		if !strings.Contains(err.Error(), "wide-first-failing") {
			t.Fatalf("run %d: error %q must stay the first rule in declaration order once it has "+
				"failed — a later failure from the other half must not displace it. Overwriting "+
				"unconditionally satisfies the two tests above, because both of those put the "+
				"right answer last in wall time, and is just as much a coin flip (#315)", i, err)
		}
	}
}
