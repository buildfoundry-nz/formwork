// phase1_pool_test.go — phase 1 (per-file checkers) must partition its pool the
// way phase 2 partitions its finalizers, and against a different resource.
//
// A SELF-BOUNDED checker takes its own internal concurrency limit INSIDE
// CheckFile. sqlparse is the shipped one: it admits at most four callers to the
// WASM parser because each go-pgquery instance costs ~250 MB resident and the
// pool never gives the memory back (#83). That admission is a BLOCKING channel
// send on the calling goroutine — internal/rules/sqlparse/parser.go's `parseSem
// <- struct{}{}` — reached synchronously from CheckFile, so in ONE flat pool the
// worker waiting for a parser slot is parked: it is still holding its file and
// takes nothing else off the queue. The bound then throttles the whole phase,
// not just the parser.
//
// Measured at the shipped default (bound 4, --workers 12) over 200 .sql files
// under sql/parses plus 4,000 .txt files under forbidden-pattern: sql alone
// 0.67s, txt alone 0.52s, both together 1.20s — sum(0.67, 0.52), not
// max(0.67, 0.52). That is #83's second acceptance criterion, dropped when the
// bound shipped without an engine-side counterpart (#315).
//
// The tests here are the same three-part shape heavy_finalizer_pool_test.go
// uses for #67: the fast half keeps its width, the operator's --workers 1
// throttle still means one file at a time, and no rule falls out of the
// partition (an un-run rule reads as a pass).
package engine_test

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// orderedFileSet builds a FileSet in the given order. memFileSet takes a map,
// and map iteration order is random — these tests depend on the self-bounded
// files being dispatched BEFORE the fast ones, which is what puts every worker
// of a flat pool inside the bounded checker.
func orderedFileSet(paths []string) *scan.FileSet {
	fset := &scan.FileSet{Root: "mem"}
	for _, p := range paths {
		fset.Files = append(fset.Files, scan.NewMemFile(p, []byte("x")))
	}
	return fset
}

// The two failure messages below are emitted BY the fakes and asserted on by
// name, so a red test says which half of the contract broke rather than "a
// finding appeared".
const (
	phase1StarvedMsg = "self-bounded checker held a phase-1 worker and the fast half never got one"
	phase1AloneMsg   = "fast checker ran this file alone — the phase-1 pool was not at full width"
)

// selfBoundedChecker models sqlparse. It takes its OWN width-1 admission before
// doing any work — the blocking send that parks the caller — and declares that
// with SelfBounded, the engine-local discriminator. It then waits for the fast
// half to reach full width, which is what makes the two-pool property testable
// without a stopwatch: under one flat pool the wait cannot be satisfied, so the
// test fails on a deadline instead of on a timing read.
type selfBoundedChecker struct {
	sem      chan struct{}
	released <-chan struct{}
	deadline time.Time
	ran      atomic.Int64
}

// SelfBounded is the discriminator engine.Run reads to route this checker into
// the bounded half of phase 1.
func (*selfBoundedChecker) SelfBounded() bool { return true }

func (c *selfBoundedChecker) CheckFile(f *scan.File) ([]rules.Match, error) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	c.ran.Add(1)
	select {
	case <-c.released:
		return nil, nil
	case <-time.After(time.Until(c.deadline)):
		return []rules.Match{{Line: 1, Message: phase1StarvedMsg}}, nil
	}
}

// widthBarrierChecker completes only when `n` files are inside its CheckFile at
// the same time. It is the direct expression of "non-SQL rules keep full
// parallelism": the count IS the width, not a proxy for it.
type widthBarrierChecker struct {
	b       *barrier
	timeout time.Duration
	ran     atomic.Int64
}

func (c *widthBarrierChecker) CheckFile(*scan.File) ([]rules.Match, error) {
	c.ran.Add(1)
	if !c.b.arrive(c.timeout) {
		return []rules.Match{{Line: 1, Message: phase1AloneMsg}}, nil
	}
	return nil, nil
}

// TestRunKeepsFastCheckersConcurrentAlongsideSelfBoundedOnes is #83's second
// acceptance criterion: the bound must not serialise the whole phase-1 pool.
//
// Four self-bounded files come FIRST in scan order, and the checker over them
// admits one caller at a time. Under one flat pool every worker takes one of
// them and parks — the fast files are never even dispatched — so the barrier
// never reaches width and both fakes report their deadline. Under two
// concurrent pools the fast half has its own workers, reaches width 4
// immediately, and releases the bounded half.
//
// This test deliberately does NOT assert that the self-bounded rule ran: a
// partition bug that dropped the bounded half entirely would leave the fast
// half looking perfect here. That hazard is TestRunPhase1PartitionRunsEveryRule's,
// and keeping it there is what makes each mutation redden exactly one test.
func TestRunKeepsFastCheckersConcurrentAlongsideSelfBoundedOnes(t *testing.T) {
	const workers = 4
	b := newBarrier(workers)
	bound := &selfBoundedChecker{
		sem:      make(chan struct{}, 1),
		released: b.ready,
		deadline: time.Now().Add(2 * time.Second),
	}
	fast := &widthBarrierChecker{b: b, timeout: 2 * time.Second}
	rls := []*config.Rule{
		mustRule(t, "self-bounded-rule", finding.SeverityError, []string{"bound/**"}, bound),
		mustRule(t, "fast-rule", finding.SeverityError, []string{"fast/**"}, fast),
	}
	paths := make([]string, 0, 2*workers)
	for i := range workers {
		paths = append(paths, fmt.Sprintf("bound/b%d.txt", i))
	}
	for i := range workers {
		paths = append(paths, fmt.Sprintf("fast/f%d.txt", i))
	}

	got, err := engine.Run(rls, orderedFileSet(paths), workers)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range got {
		if strings.Contains(fd.Message, phase1StarvedMsg) || strings.Contains(fd.Message, phase1AloneMsg) {
			t.Fatalf("%s: %s — a checker that bounds itself inside CheckFile parks the phase-1 "+
				"worker that called it, so one flat pool lets the bound throttle every OTHER rule too; "+
				"phase 1 must dispatch self-bounded rules through their own pool, concurrently with the "+
				"rest (#83 acceptance criterion 2, #315)", fd.RuleID, fd.Message)
		}
	}
	if n := fast.ran.Load(); n != workers {
		t.Fatalf("fast checker ran on %d file(s), want %d: the fast half of the partition must still "+
			"see every file in its scope", n, workers)
	}
}

// phase1Holder occupies CheckFile long enough that any peer dispatched
// concurrently is guaranteed to overlap it. Without the hold two checkers can
// interleave fast enough that a peak of 1 proves nothing.
type phase1Holder struct {
	p    *peakTracker
	hold time.Duration
	kind string
}

func (c *phase1Holder) CheckFile(f *scan.File) ([]rules.Match, error) {
	c.p.enter()
	time.Sleep(c.hold)
	c.p.leave()
	return []rules.Match{{Line: 1, Message: c.kind + " checker ran: " + f.Path()}}, nil
}

// phase1BoundHolder is a phase1Holder that declares itself self-bounded, so it
// is dispatched through the bounded half of the partition.
type phase1BoundHolder struct{ phase1Holder }

func (*phase1BoundHolder) SelfBounded() bool { return true }

// countRan counts findings whose message starts with prefix.
func countRan(got []finding.Finding, prefix string) int {
	n := 0
	for _, fd := range got {
		if strings.HasPrefix(fd.Message, prefix) {
			n++
		}
	}
	return n
}

// TestRunPhase1SerialisesBothHalvesAtWorkerOne pins the operator's escape
// hatch across the new partition, the same contract
// TestRunWorkersOneSerialisesHeavyFinalizers pins for phase 2: --workers 1 must
// still mean ONE file in flight at a time. Two concurrent pools of width 1 are
// two files in flight, which is exactly twice what the operator asked for — and
// on the machine they throttled to protect, the second one is the whole point
// of the throttle.
//
// The assertion is EXACT. A peak above 1 is the throttle exceeded; a peak below
// 1 would mean nothing ran, which the per-rule counts below catch separately
// rather than letting a silent skip read as a clean serialisation.
func TestRunPhase1SerialisesBothHalvesAtWorkerOne(t *testing.T) {
	const files = 4
	p := &peakTracker{}
	bound := &phase1BoundHolder{phase1Holder{p: p, hold: 25 * time.Millisecond, kind: "self-bounded"}}
	fast := &phase1Holder{p: p, hold: 25 * time.Millisecond, kind: "fast"}
	rls := []*config.Rule{
		mustRule(t, "self-bounded-rule", finding.SeverityError, []string{"**"}, bound),
		mustRule(t, "fast-rule", finding.SeverityError, []string{"**"}, fast),
	}
	paths := make([]string, 0, files)
	for i := range files {
		paths = append(paths, fmt.Sprintf("f%d.txt", i))
	}

	got, err := engine.Run(rls, orderedFileSet(paths), 1)
	if err != nil {
		t.Fatal(err)
	}
	if n := countRan(got, "self-bounded checker ran: "); n != files {
		t.Fatalf("self-bounded checker ran on %d file(s), want %d", n, files)
	}
	if n := countRan(got, "fast checker ran: "); n != files {
		t.Fatalf("fast checker ran on %d file(s), want %d", n, files)
	}
	if n := p.max(); n != 1 {
		t.Fatalf("peak concurrent CheckFile across both phase-1 halves = %d, want exactly 1 under "+
			"--workers 1: the partition must MERGE into a single pool at that width, exactly as the "+
			"two heavy finalizer pools do, or the operator's throttle buys them two files in flight "+
			"instead of one (#315)", n)
	}
}

// phase1Emitter emits one match per file naming itself, so a run's whole
// finding set is known up front and any rule the partition failed to dispatch
// is visible as a missing pair rather than as silence.
type phase1Emitter struct{}

func (*phase1Emitter) CheckFile(f *scan.File) ([]rules.Match, error) {
	return []rules.Match{{Line: 1, Message: "ran on " + f.Path()}}, nil
}

// phase1BoundEmitter routes through the bounded half.
type phase1BoundEmitter struct{ phase1Emitter }

func (*phase1BoundEmitter) SelfBounded() bool { return true }

// TestRunPhase1PartitionRunsEveryRule is the hazard the partition introduces
// and the one phase 2 already names: "No finalizer can land in neither pool (an
// un-run rule would read as a pass)." A rule dropped from dispatch emits no
// findings, and no findings is how the exit-code contract spells PASS — so the
// failure mode of a mis-partition is a silent green, not an error.
//
// Both widths are checked. They are different code paths: above 1 the two
// slices are dispatched through two pools, at 1 the merged branch dispatches
// rls, and a slice lost from either would be invisible in the other.
func TestRunPhase1PartitionRunsEveryRule(t *testing.T) {
	const files = 3
	// Interleaved declaration order: bound, wide, bound, wide.
	ids := []string{"bound-0", "wide-0", "bound-1", "wide-1"}
	build := func() []*config.Rule {
		rls := make([]*config.Rule, 0, len(ids))
		for _, id := range ids {
			var c rules.Checker = &phase1Emitter{}
			if strings.HasPrefix(id, "bound-") {
				c = &phase1BoundEmitter{}
			}
			rls = append(rls, mustRule(t, id, finding.SeverityError, []string{"**"}, c))
		}
		return rls
	}
	paths := make([]string, 0, files)
	for i := range files {
		paths = append(paths, fmt.Sprintf("f%d.txt", i))
	}
	want := make([]string, 0, len(ids)*files)
	for _, id := range ids {
		for _, p := range paths {
			want = append(want, id+":"+p)
		}
	}
	sort.Strings(want)

	for _, workers := range []int{4, 1} {
		got, err := engine.Run(build(), orderedFileSet(paths), workers)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(got))
		for _, fd := range got {
			keys = append(keys, fd.RuleID+":"+fd.Path)
		}
		sort.Strings(keys)
		if strings.Join(keys, "|") != strings.Join(want, "|") {
			t.Fatalf("--workers %d: phase 1 evaluated %v, want %v: every rule must land in exactly one "+
				"half of the partition — a rule dropped from dispatch emits nothing, and nothing is "+
				"how the exit-code contract spells PASS (#315)", workers, keys, want)
		}
	}
}
