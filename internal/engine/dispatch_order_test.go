// dispatch_order_test.go — phase 2 dispatches each pool LONGEST-POLE-FIRST,
// not in declaration order (#26).
//
// The defect is a scheduling one, not a correctness one. A pool hands its
// indices to an unbuffered channel in the order it iterates them, so the first
// `width` rules it sends are the first `width` rules that start. When one rule
// spans most of the window and is declared late, the pool spends its opening
// seconds on cheap rules and then ends waiting on a long pole that started
// last. Measured in the validating port: `check` is 72-80% of the guardrail
// step, two rules dominate it (241-429s and 238-462s), mean concurrency is
// 3.1-3.3 of 4 slots, and one of the two does not begin until t+59/72/75s.
//
// Two facts make re-ordering safe, and both are pinned elsewhere rather than
// assumed here: findings are sorted before they are returned
// (TestRunAppliesScopeAndFillsPathAndSorts), and the engine error a run reports
// is chosen by `fins` INDEX, never by completion order
// (TestRunFinalizerErrorIsDeterministicAcrossPools). What this file adds is the
// third obligation, which is the one a re-order can break on its own: every
// rule still runs, exactly once, in every pool, under every ordering — because
// a rule dropped from dispatch emits nothing, and nothing is how the
// exit-code contract spells PASS.
//
// Phase 1 is deliberately NOT re-ordered and no test here asks it to be. Its
// pool dispatches FILE indices and each worker evaluates every applicable rule
// against the file it took, so rule order inside that loop changes no rule's
// start time — while the merged --workers 1 branch relies on `allRls` being in
// declaration order (see the comment there).
package engine_test

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// longPoleFinalizer is the rule that dominates the window. It signals the
// moment it STARTS, which is the only event the ordering claim is about — not
// when it finishes, and not how long it takes.
type longPoleFinalizer struct {
	started chan struct{}
	once    sync.Once
}

func (*longPoleFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func (p *longPoleFinalizer) Finalize() []rules.Match {
	p.once.Do(func() { close(p.started) })
	return []rules.Match{{Message: "long pole ran"}}
}

// treeCostLongPole is the same rule declaring `cost: tree`, so the declared-cost
// fallback has something to rank above the fast rules around it. It stays out of
// the heavy partition (that is CostHeavy only), so it is dispatched through the
// SAME pool as the cheap rules and its start time is decided by order alone.
type treeCostLongPole struct{ longPoleFinalizer }

func (*treeCostLongPole) Cost() rules.Cost { return rules.CostTree }

// cheapAfterPole is one of the ~2,900 rules that finish in the long pole's
// shadow. It reports a finding only when it ran while the long pole had NOT yet
// started — which is precisely the wasted head start #26 is about, expressed as
// an assertion instead of a stopwatch. Nothing here compares wall-clock
// durations, so the test cannot flake into passing on a fast machine.
type cheapAfterPole struct {
	started <-chan struct{}
	timeout time.Duration
	id      string
}

func (*cheapAfterPole) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func (c *cheapAfterPole) Finalize() []rules.Match {
	select {
	case <-c.started:
		return nil
	case <-time.After(c.timeout):
		return []rules.Match{{Message: "ran before the long pole started: " + c.id}}
	}
}

// poleHeadStartRules builds one long pole declared LAST behind n cheap rules —
// the shape the issue describes, where declaration order is the worst possible
// order. pole is supplied by the caller so the two orderings (prior durations,
// declared cost) can use the same corpus with a different discriminator.
func poleHeadStartRules(t *testing.T, n int, pole rules.Checker, started <-chan struct{}) []*config.Rule {
	t.Helper()
	rls := make([]*config.Rule, 0, n+1)
	for i := range n {
		id := fmt.Sprintf("cheap-%d", i)
		rls = append(rls, mustRule(t, id, finding.SeverityError, []string{"**"},
			&cheapAfterPole{started: started, timeout: 2 * time.Second, id: id}))
	}
	rls = append(rls, mustRule(t, "long-pole", finding.SeverityError, []string{"**"}, pole))
	return rls
}

// assertPoleStartedFirst fails naming every cheap rule that had to run without
// the long pole in flight, and fails too if the pole never ran at all — a
// re-order that DROPPED the pole would otherwise satisfy every other assertion
// here by keeping the cheap rules quiet.
func assertPoleStartedFirst(t *testing.T, got []finding.Finding) {
	t.Helper()
	var early []string
	poleRan := false
	for _, fd := range got {
		if strings.HasPrefix(fd.Message, "ran before the long pole started: ") {
			early = append(early, fd.RuleID)
		}
		if fd.Message == "long pole ran" {
			poleRan = true
		}
	}
	if !poleRan {
		t.Fatal("the long pole never ran: a dispatch order that drops a rule is a silent skip, " +
			"and a skipped rule reads as a pass (exit-code contract)")
	}
	if len(early) > 0 {
		t.Fatalf("%d rule(s) occupied the pool before the long pole started (%s): the pool must "+
			"dispatch its longest rule first, so the window is bounded by that rule rather than by "+
			"that rule plus the head start it spent queued (#26)", len(early), strings.Join(early, ", "))
	}
}

// TestRunStartsThePriorLongPoleFirst is #26's acceptance criterion at the unit
// level. Seven cheap rules are declared ahead of one long pole; the pool is
// four wide, so in declaration order the first four cheap rules all start while
// the pole is still queued. A prior-durations hint naming the pole as the
// slowest rule must move it to the front of the dispatch.
//
// The hint names a rule the run does not hold ("retired-rule") as well, because
// a durations report is a PREVIOUS run's and rule sets drift between them. A
// stale entry must be inert, not an error and not a reordering of something
// else.
func TestRunStartsThePriorLongPoleFirst(t *testing.T) {
	const workers = 4
	started := make(chan struct{})
	pole := &longPoleFinalizer{started: started}
	rls := poleHeadStartRules(t, 7, pole, started)

	prior := engine.Timing{
		"long-pole":    9 * time.Second,
		"retired-rule": 99 * time.Second,
		"cheap-3":      2 * time.Millisecond,
	}
	got, _, err := engine.RunTimedHinted(rls, memFileSet(map[string]string{"a.txt": "x"}), workers, nil, prior)
	if err != nil {
		t.Fatal(err)
	}
	assertPoleStartedFirst(t, got)
}

// TestRunFallsBackToDeclaredCostOrder is the half that needs no state. Nobody
// has to run `check` once to feed it a report: `cost:` is declared in the rule
// file (v0.6.3), the partition already reads it to decide WHICH pool a rule
// goes to, and ranking by it inside a pool costs nothing. The pole here declares
// `cost: tree` and no durations are supplied at all.
func TestRunFallsBackToDeclaredCostOrder(t *testing.T) {
	const workers = 4
	started := make(chan struct{})
	pole := &treeCostLongPole{longPoleFinalizer{started: started}}
	rls := poleHeadStartRules(t, 7, pole, started)

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), workers)
	if err != nil {
		t.Fatal(err)
	}
	assertPoleStartedFirst(t, got)
}

// orderEmitter emits exactly one match naming itself and counts its own
// invocations. Both halves matter: the match proves the rule RAN (a dropped
// rule is a silent pass), and the counter proves it ran ONCE (a re-order that
// duplicated an index would double-report every finding it touched, which the
// finding set alone cannot distinguish from a rule that emits twice).
type orderEmitter struct {
	id   string
	runs *atomic.Int32
}

func (*orderEmitter) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func (e *orderEmitter) Finalize() []rules.Match {
	e.runs.Add(1)
	return []rules.Match{{Message: "ran: " + e.id}}
}

// The three cost shapes the phase-2 partition sorts rules into. Each is
// dispatched through its own pool, so "every rule runs exactly once in every
// pool" needs one of each.
type treeEmitter struct{ orderEmitter }

func (*treeEmitter) Cost() rules.Cost { return rules.CostTree }

type wideHeavyEmitter struct{ orderEmitter }

func (*wideHeavyEmitter) Cost() rules.Cost   { return rules.CostHeavy }
func (*wideHeavyEmitter) ProcessBound() bool { return false }

type boundHeavyEmitter struct{ orderEmitter }

func (*boundHeavyEmitter) Cost() rules.Cost { return rules.CostHeavy }

// mixedPoolRules builds a corpus that populates all three phase-2 pools, in an
// interleaved declaration order so a partition that lost a whole slice is
// visible rather than masked by a neighbouring one.
func mixedPoolRules(t *testing.T, runs map[string]*atomic.Int32) []*config.Rule {
	t.Helper()
	var rls []*config.Rule
	add := func(id string, mk func(orderEmitter) rules.Checker) {
		c := &atomic.Int32{}
		runs[id] = c
		rls = append(rls, mustRule(t, id, finding.SeverityError, []string{"**"}, mk(orderEmitter{id: id, runs: c})))
	}
	for i := range 3 {
		add(fmt.Sprintf("fast-%d", i), func(e orderEmitter) rules.Checker { return &e })
		add(fmt.Sprintf("tree-%d", i), func(e orderEmitter) rules.Checker { return &treeEmitter{e} })
		add(fmt.Sprintf("wide-heavy-%d", i), func(e orderEmitter) rules.Checker { return &wideHeavyEmitter{e} })
		add(fmt.Sprintf("bound-heavy-%d", i), func(e orderEmitter) rules.Checker { return &boundHeavyEmitter{e} })
	}
	return rls
}

// dispatchOrderings are the hints the tests below sweep. Each must produce the
// same verdict from the same corpus; what changes is only which rule the pool
// sends first.
func dispatchOrderings() map[string]engine.Timing {
	return map[string]engine.Timing{
		"no hint (declared cost only)": nil,
		"empty hint":                   {},
		"declaration order": {
			"fast-0": 12 * time.Second, "tree-0": 11 * time.Second,
			"wide-heavy-0": 10 * time.Second, "bound-heavy-0": 9 * time.Second,
			"fast-1": 8 * time.Second, "tree-1": 7 * time.Second,
			"wide-heavy-1": 6 * time.Second, "bound-heavy-1": 5 * time.Second,
			"fast-2": 4 * time.Second, "tree-2": 3 * time.Second,
			"wide-heavy-2": 2 * time.Second, "bound-heavy-2": 1 * time.Second,
		},
		"reversed": {
			"fast-0": 1 * time.Second, "tree-0": 2 * time.Second,
			"wide-heavy-0": 3 * time.Second, "bound-heavy-0": 4 * time.Second,
			"fast-1": 5 * time.Second, "tree-1": 6 * time.Second,
			"wide-heavy-1": 7 * time.Second, "bound-heavy-1": 8 * time.Second,
			"fast-2": 9 * time.Second, "tree-2": 10 * time.Second,
			"wide-heavy-2": 11 * time.Second, "bound-heavy-2": 12 * time.Second,
		},
		"partial, ties and a stale id": {
			"bound-heavy-2": 30 * time.Second,
			"fast-2":        30 * time.Second,
			"tree-1":        30 * time.Second,
			"deleted-rule":  90 * time.Second,
		},
	}
}

// TestRunDispatchOrderRunsEveryRuleExactlyOnceInEveryPool is the invariant a
// re-order is capable of breaking by itself, and it is the analogue of phase
// 1's TestRunPhase1PartitionRunsEveryRule and of phase 2's own
// assertHeavyAllRan. Sorting is a permutation only if it keeps every element;
// an ordering that dropped one would emit no finding for that rule, and no
// finding is a PASS.
//
// Both widths are swept, because they are different code paths: above 1 the
// three slices are dispatched through three pools, at 1 the two heavy slices
// are merged into one, and a slice lost from either would be invisible in the
// other.
func TestRunDispatchOrderRunsEveryRuleExactlyOnceInEveryPool(t *testing.T) {
	for _, workers := range []int{1, 4} {
		for name, prior := range dispatchOrderings() {
			t.Run(fmt.Sprintf("workers=%d/%s", workers, name), func(t *testing.T) {
				runs := map[string]*atomic.Int32{}
				rls := mixedPoolRules(t, runs)
				got, _, err := engine.RunTimedHinted(rls, memFileSet(map[string]string{"a.txt": "x"}), workers, nil, prior)
				if err != nil {
					t.Fatal(err)
				}
				for id, c := range runs {
					if n := c.Load(); n != 1 {
						t.Errorf("rule %s ran %d time(s), want exactly 1: a dispatch order that drops a "+
							"rule is a silent pass, and one that duplicates it double-reports every "+
							"finding it produces", id, n)
					}
				}
				if len(got) != len(runs) {
					t.Fatalf("findings = %d, want %d (one per rule): %v", len(got), len(runs), got)
				}
			})
		}
	}
}

// TestRunFindingsAreIdenticalUnderEveryDispatchOrder is the output contract:
// dispatch order must not be observable in what a run reports. It is the
// engine-side half of the promise `--durations` makes at the CLI — the flag may
// change how long a run takes and nothing else.
func TestRunFindingsAreIdenticalUnderEveryDispatchOrder(t *testing.T) {
	render := func(fds []finding.Finding) string {
		var b strings.Builder
		for _, fd := range fds {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t%s\t%v\n", fd.RuleID, fd.Severity, fd.Path, fd.Line, fd.Message, fd.Suppressed)
		}
		return b.String()
	}
	var want string
	for _, workers := range []int{1, 4} {
		for name, prior := range dispatchOrderings() {
			runs := map[string]*atomic.Int32{}
			rls := mixedPoolRules(t, runs)
			got, _, err := engine.RunTimedHinted(rls, memFileSet(map[string]string{"a.txt": "x"}), workers, nil, prior)
			if err != nil {
				t.Fatalf("workers=%d %s: %v", workers, name, err)
			}
			out := render(got)
			if want == "" {
				want = out
				continue
			}
			if out != want {
				t.Fatalf("workers=%d %s: findings differ from the reference ordering.\ngot:\n%s\nwant:\n%s",
					workers, name, out, want)
			}
		}
	}
	if want == "" {
		t.Fatal("no ordering was exercised — the sweep is vacuous")
	}
}

// slowErrFinalizer errors after a delay, so a rule dispatched FIRST can still
// be the LAST to fail. It is the vector that separates "the engine reports the
// earliest-declared failing rule" from "the engine reports whichever rule
// failed first".
type slowErrFinalizer struct {
	err   error
	delay time.Duration
}

func (*slowErrFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (s *slowErrFinalizer) FinalizeErr(rules.FinalizeContext) ([]rules.Match, error) {
	time.Sleep(s.delay)
	return nil, s.err
}

// TestRunFinalizerErrorIgnoresDispatchOrder extends
// TestRunFinalizerErrorIsDeterministicAcrossPools to the new degree of freedom.
// That test races two rules whose declaration order and dispatch order agree;
// here the hint deliberately sends the LATER-declared rule first, and at
// --workers 1 the pool is serial, so the second rule's error is genuinely the
// first one the engine sees. Declaration order must still decide.
func TestRunFinalizerErrorIgnoresDispatchOrder(t *testing.T) {
	fset := memFileSet(map[string]string{"a.txt": "x"})
	prior := engine.Timing{"declared-second": 60 * time.Second}
	for i := range 10 {
		rls := []*config.Rule{
			mustRule(t, "declared-first", finding.SeverityError, []string{"**"},
				&slowErrFinalizer{err: errFake, delay: 5 * time.Millisecond}),
			mustRule(t, "declared-second", finding.SeverityError, []string{"**"},
				&fakeErrFinalizer{err: errFake}),
		}
		_, _, err := engine.RunTimedHinted(rls, fset, 1, nil, prior)
		if err == nil {
			t.Fatalf("run %d: expected an engine error", i)
		}
		if !strings.Contains(err.Error(), "declared-first") {
			t.Fatalf("run %d: error %q must name the first rule in DECLARATION order, however the "+
				"pool chose to dispatch them (#26)", i, err)
		}
	}
}
