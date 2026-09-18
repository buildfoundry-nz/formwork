package engine

import (
	"slices"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// dispatchOrder re-orders one phase-2 pool's index slice LONGEST-POLE-FIRST.
//
// It returns a NEW slice rather than sorting in place. Nothing reads the
// partition slices after dispatch today, so an in-place sort would be harmless
// as the code stands — the copy is there because those slices are the
// partition's record of which rule went to which pool, and a function that
// silently reorders its caller's data is a trap for whoever adds a second
// consult of them.
//
// WHY ORDER AT ALL. A pool sends its indices to an unbuffered channel, so the
// first `width` it sends are the first `width` that start. Declaration order
// therefore decides which rules get the opening slots, and a corpus does not
// declare its rules in cost order — nothing asks it to. Measured in the
// validating port over 12+ CI runs on a 4 vCPU runner: `check` is 72-80% of
// the guardrail step; two rules span nearly the whole window (241-429s and
// 238-462s) while the other ~2,900 finish inside their shadow at a mean
// concurrency of 3.1-3.3 of 4 slots; and one of the two does not start until
// t+59/72/75s because the pool works through the cheap rules declared ahead of
// it first. Nothing in that window is idle and no rule is slower than it needs
// to be — the only recoverable slack is the head start (#26).
//
// WHAT IT ORDERS BY, in order:
//
//  1. A prior run's MEASURED duration for that rule, longest first. This is
//     what `check --durations <report>` supplies: v0.6.2 already emits per-rule
//     durations into the JSON report, so a CI job can feed its predecessor's
//     report back in and needs no state of its own.
//  2. The DECLARED cost class, heaviest first (rules.Rank: heavy > tree >
//     range > fast). v0.6.3 put `cost:` on command rules for --cost-max, which
//     FILTERS by it; this is the ordering half of the same declaration. It
//     needs no previous run, so it is what an adopter gets for free.
//  3. Nothing — the sort is STABLE, so rules the first two keys cannot separate
//     are dispatched in exactly the order the pool was given them. That is the
//     whole of what a corpus declaring no costs and supplying no report sees:
//     today's dispatch, unchanged.
//
// A rule the hint does not name is UNKNOWN, not fast, so it sorts after every
// measured rule rather than being ranked against them at zero. A durations
// report describes the run that wrote it, and this run may hold rules added
// since; ranking a new rule as instantaneous would systematically bury exactly
// the rules nobody has measured yet.
//
// ORDER IS NOT A VERDICT, and must never become one. It cannot change which
// findings a run reports (finding.Sort orders the output), nor which engine
// error it reports (finErrIdx ranks by `fins` index, not by completion), nor
// which rules run — this is a permutation, pinned by
// TestDispatchOrderIsAPermutationAndDoesNotMutateItsInput. A stale, partial or
// absent hint is therefore
// always safe: the worst a wrong duration can do is spend the window in the
// order the engine uses today.
func dispatchOrder(idx []int, fins []*config.Rule, prior Timing) []int {
	out := slices.Clone(idx)
	slices.SortStableFunc(out, func(a, b int) int {
		ra, rb := fins[a], fins[b]
		da, oka := prior[ra.ID]
		db, okb := prior[rb.ID]
		switch {
		case oka && !okb:
			return -1
		case !oka && okb:
			return 1
		case oka && okb && da != db:
			return longerFirst(da, db)
		}
		return rules.Rank(rb.Cost()) - rules.Rank(ra.Cost())
	})
	return out
}

// longerFirst is the descending comparison for durations, spelled out rather
// than returned as a subtraction. time.Duration counts NANOSECONDS, so any two
// rules more than ~2.1 seconds apart overflow a 32-bit int — and the
// int(b - a) idiom would then hand slices.SortStableFunc a truncated value
// whose sign is arbitrary, which is exactly the pair of rules this ordering
// exists to separate. Callers guarantee a != b before reaching here.
func longerFirst(a, b time.Duration) int {
	if a > b {
		return -1
	}
	return 1
}
