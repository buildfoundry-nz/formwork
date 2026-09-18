// dispatch_order_internal_test.go — the comparator behind phase 2's dispatch,
// asserted directly rather than inferred from which rule happened to start
// first. dispatch_order_test.go proves the engine HONOURS the order; this file
// proves the order is the one #26 asked for, including the two arms that a
// timing-based test cannot distinguish: what happens to a rule the hint does
// not name, and what happens to a tie.
package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// costChecker is a finalizer declaring a cost class and nothing else. The
// comparator reads only the rule's ID and its Cost(), so this is the whole
// surface it needs.
type costChecker struct{ cost rules.Cost }

func (*costChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (*costChecker) Finalize() []rules.Match                     { return nil }
func (c *costChecker) Cost() rules.Cost                          { return c.cost }

// finsWithCosts builds a `fins`-shaped slice: id i at index i, each with its
// declared class.
func finsWithCosts(t *testing.T, spec ...struct {
	id   string
	cost rules.Cost
},
) []*config.Rule {
	t.Helper()
	out := make([]*config.Rule, 0, len(spec))
	for _, s := range spec {
		r, err := config.New(s.id, "fake", finding.SeverityError, "fix it", []string{"**"}, nil, nil, &costChecker{cost: s.cost})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

type costSpec = struct {
	id   string
	cost rules.Cost
}

// ids renders a dispatch order as the rule ids it sends, which is what the
// assertions below are actually about — an index list proves nothing on its own
// once the input is not 0..n-1.
func ids(idx []int, fins []*config.Rule) []string {
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		out = append(out, fins[i].ID)
	}
	return out
}

func equalIDs(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestDispatchOrderSendsTheLongestKnownRuleFirst is the primary key: a measured
// duration from a previous run outranks everything else, longest first.
func TestDispatchOrderSendsTheLongestKnownRuleFirst(t *testing.T) {
	fins := finsWithCosts(t,
		costSpec{"cheap", rules.CostFast},
		costSpec{"middling", rules.CostFast},
		costSpec{"long-pole", rules.CostFast},
	)
	prior := Timing{"cheap": 2 * time.Millisecond, "middling": 4 * time.Second, "long-pole": 400 * time.Second}
	got := ids(dispatchOrder([]int{0, 1, 2}, fins, prior), fins)
	if !equalIDs(got, "long-pole", "middling", "cheap") {
		t.Fatalf("dispatch order = %v, want longest-first [long-pole middling cheap]", got)
	}
}

// TestDispatchOrderPutsUnmeasuredRulesAfterMeasuredOnes pins the arm a hint
// cannot state. A durations report names the rules of the run that WROTE it;
// this run may hold rules added since. An unmeasured rule is not a fast rule —
// it is an unknown one — so it must not be promoted above a rule measured at
// minutes, and it must not be pushed below one measured at microseconds either.
// After the measured ones, in declaration order, is the only answer that
// invents no information.
func TestDispatchOrderPutsUnmeasuredRulesAfterMeasuredOnes(t *testing.T) {
	fins := finsWithCosts(t,
		costSpec{"new-rule", rules.CostFast},
		costSpec{"measured-fast", rules.CostFast},
		costSpec{"another-new-rule", rules.CostFast},
		costSpec{"measured-slow", rules.CostFast},
	)
	prior := Timing{"measured-fast": time.Millisecond, "measured-slow": time.Minute}
	got := ids(dispatchOrder([]int{0, 1, 2, 3}, fins, prior), fins)
	if !equalIDs(got, "measured-slow", "measured-fast", "new-rule", "another-new-rule") {
		t.Fatalf("dispatch order = %v, want the measured rules first (longest first) and the "+
			"unmeasured ones after them in declaration order", got)
	}
}

// TestDispatchOrderFallsBackToDeclaredCost is the no-state half: with no hint
// at all, the declared class ranks the pool (heavy > tree > range > fast, the
// order rules.Rank already defines for --cost-max). Nothing has to be measured
// first, and nothing has to be carried between runs.
func TestDispatchOrderFallsBackToDeclaredCost(t *testing.T) {
	fins := finsWithCosts(t,
		costSpec{"a-fast", rules.CostFast},
		costSpec{"a-tree", rules.CostTree},
		costSpec{"a-range", rules.CostRange},
		costSpec{"a-heavy", rules.CostHeavy},
	)
	got := ids(dispatchOrder([]int{0, 1, 2, 3}, fins, nil), fins)
	if !equalIDs(got, "a-heavy", "a-tree", "a-range", "a-fast") {
		t.Fatalf("dispatch order = %v, want declared-cost order [a-heavy a-tree a-range a-fast]", got)
	}
}

// TestDispatchOrderIsStableAcrossTies is what keeps "no hint" from becoming a
// reshuffle. Most of a real pool ties: every rule in the heavy pools declares
// the same class, and a corpus that declares no `cost:` at all ties on every
// rule it has. A stable sort is the difference between those pools dispatching
// exactly as they do today and dispatching in whatever order a sort happened
// to leave them.
//
// THE SHAPE IS LOAD-BEARING, and the first draft of this test did not have it.
// slices.SortFunc — the unstable sibling one letter away — returns early on a
// run of wholly equal elements, so a corpus of N tied rules comes back in input
// order from BOTH calls however large N is, and the test passed identically
// against the mutation. It takes DISTINCT groups with ties inside them to make
// pdqsort actually partition, which is also the realistic case: a fast pool
// holding `tree` rules interleaved with `fast` ones. Sixteen rules alternating
// between two classes is past the insertion-sort threshold and reorders under
// the unstable call.
func TestDispatchOrderIsStableAcrossTies(t *testing.T) {
	const n = 16
	spec := make([]costSpec, 0, n)
	for i := range n {
		cost := rules.CostFast
		if i%2 == 0 {
			cost = rules.CostTree
		}
		spec = append(spec, costSpec{fmt.Sprintf("r%02d", i), cost})
	}
	fins := finsWithCosts(t, spec...)

	// grouped renders the expected answer: the tree rules in the order the
	// COMPARATOR WAS GIVEN them, then the fast ones in that same order.
	grouped := func(input []int) []string {
		var heavier, lighter []string
		for _, i := range input {
			if fins[i].Cost() == rules.CostTree {
				heavier = append(heavier, fins[i].ID)
			} else {
				lighter = append(lighter, fins[i].ID)
			}
		}
		return append(heavier, lighter...)
	}

	declared := make([]int, n)
	for i := range n {
		declared[i] = i
	}
	if got, want := ids(dispatchOrder(declared, fins, nil), fins), grouped(declared); !equalIDs(got, want...) {
		t.Fatalf("dispatch order = %v, want %v: rules the comparator cannot separate keep their order", got, want)
	}

	// Reversed input, because at --workers 1 the two heavy slices are MERGED
	// and the slice reaching the comparator runs wide-then-bound rather than in
	// declaration order. Ties must preserve the order the pool was GIVEN, not
	// silently re-sort into declaration order — that would change today's
	// --workers 1 dispatch for a run that supplied no hint at all.
	merged := make([]int, 0, n)
	for i := n - 1; i >= 0; i-- {
		merged = append(merged, i)
	}
	if got, want := ids(dispatchOrder(merged, fins, nil), fins), grouped(merged); !equalIDs(got, want...) {
		t.Fatalf("dispatch order over a merged slice = %v, want %v", got, want)
	}

	// Equal MEASURED durations tie the same way. The comparator must fall
	// through to the declared class and then stop, rather than reshuffling
	// rules it rated identically — a run whose report says every rule took the
	// same whole millisecond (the report's resolution) must dispatch exactly as
	// one with no report at all.
	prior := Timing{}
	for _, r := range fins {
		prior[r.ID] = time.Second
	}
	if got, want := ids(dispatchOrder(declared, fins, prior), fins), grouped(declared); !equalIDs(got, want...) {
		t.Fatalf("dispatch order with equal durations = %v, want %v", got, want)
	}
}

// TestDispatchOrderIsAPermutationAndDoesNotMutateItsInput is the fail-open
// guard at the comparator's own altitude. A pool dispatches exactly what this
// function returns, so an element lost here is a rule that never runs — and a
// rule that never runs emits no finding, which the exit-code contract reads as
// a pass. The no-mutation half is defensive rather than a live bug: nothing
// reads the partition slices after dispatch as the code stands, so it pins the
// contract before a second consult of them exists rather than after.
func TestDispatchOrderIsAPermutationAndDoesNotMutateItsInput(t *testing.T) {
	fins := finsWithCosts(t,
		costSpec{"r0", rules.CostFast},
		costSpec{"r1", rules.CostHeavy},
		costSpec{"r2", rules.CostTree},
		costSpec{"r3", rules.CostFast},
		costSpec{"r4", rules.CostRange},
	)
	in := []int{0, 1, 2, 3, 4}
	prior := Timing{"r3": time.Hour}
	got := dispatchOrder(in, fins, prior)

	if want := []int{0, 1, 2, 3, 4}; !equalInts(in, want) {
		t.Fatalf("input slice was mutated: %v, want %v", in, want)
	}
	seen := map[int]int{}
	for _, i := range got {
		seen[i]++
	}
	if len(got) != len(in) {
		t.Fatalf("dispatch order has %d entries, want %d: a dropped index is a rule that never runs", len(got), len(in))
	}
	for _, i := range in {
		if seen[i] != 1 {
			t.Fatalf("index %d appears %d time(s) in the dispatch order, want exactly 1", i, seen[i])
		}
	}
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
