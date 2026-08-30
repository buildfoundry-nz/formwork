// heavy_finalizer_pool_test.go — phase 2's pool must bound HEAVY finalizers
// separately from fast ones. A heavy rule (`command`, `git-diff`) shells out to
// a whole-tree tool whose cost is per-process, not per-core: measured on the
// validating port, one `--lane ci` run forked four resolved-AST Dart analyzer
// children at once, 3.2-8.6 GB each, ~20 GB from a single formwork invocation —
// enough to hard-hang a 24 GB machine. `rules.CostOf` already classifies these
// (config.Rule.Cost), and engine.Run computed it and then dispatched every
// finalizer through one pool sized like phase 1 regardless (#67).
//
// The contract these tests pin has two halves, and both matter: no more than
// TWO heavy finalizers ever overlap (the width is measured, not guessed — see
// the heavyFinalizerWorkers comment), AND bounding them does not cost fast
// finalizers their parallelism. A fix that serialises phase 2 wholesale would
// satisfy the first and quietly undo finalizer_pool_test.go's reason for
// existing.
package engine_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// peakTracker records the greatest number of participants simultaneously inside
// the guarded region. It is the direct analogue of #67's acceptance criterion
// ("never more than K heavy subprocesses alive at once") — the count IS the
// thing being asserted, not a proxy for it.
type peakTracker struct {
	mu   sync.Mutex
	live int
	peak int
}

func (p *peakTracker) enter() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live++
	if p.live > p.peak {
		p.peak = p.live
	}
}

func (p *peakTracker) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live--
}

func (p *peakTracker) max() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peak
}

// heavyFinalizer declares CostHeavy and holds its "subprocess" open long enough
// that any peer dispatched concurrently is guaranteed to overlap it. The hold
// is what makes an unbounded pool observable: without it, four finalizers can
// interleave so fast that a peak of 1 proves nothing.
type heavyFinalizer struct {
	p    *peakTracker
	hold time.Duration
	id   string
}

func (*heavyFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (*heavyFinalizer) Cost() rules.Cost                            { return rules.CostHeavy }

// Finalize emits a match naming itself. That is not decoration: without it a
// bug that routed heavy rules into NEITHER pool would leave peak at 0, which is
// <= 1, and every assertion below would pass while the rules never ran — a
// silent skip reading as a clean bound. The partition is the exact place such a
// bug would live, so both tests assert the work HAPPENED as well as that it was
// bounded.
func (f *heavyFinalizer) Finalize() []rules.Match {
	f.p.enter()
	time.Sleep(f.hold)
	f.p.leave()
	return []rules.Match{{Message: "heavy finalizer ran: " + f.id}}
}

// assertHeavyAllRan fails unless every heavy rule emitted its match.
func assertHeavyAllRan(t *testing.T, got []finding.Finding, want int) {
	t.Helper()
	n := 0
	for _, fd := range got {
		if strings.HasPrefix(fd.Message, "heavy finalizer ran: ") {
			n++
		}
	}
	if n != want {
		t.Fatalf("heavy finalizers that ran = %d, want %d: a cost partition that drops a rule "+
			"is a silent skip, and a skipped rule reads as a pass (exit-code contract)", n, want)
	}
}

// TestRunBoundsHeavyFinalizersToTwo is #67's memory contract. Four heavy
// rules, a pool four wide: unbounded, all four enter Finalize together and the
// peak is 4, which downstream is four analyzer processes and ~20 GB. The
// assertion is EXACT — peak must be 2, not <= 2 — because both directions are
// defects: 3+ is the memory bound broken, and 1 is the pool quietly narrower
// than declared (paying serial wall-time the width was chosen to avoid, and an
// assertion that could never fail against K=1 would be vacuous).
//
// If the ==2 half ever flakes on a starved runner (a worker thread stalled
// past the 50ms hold), do NOT weaken it to <=2 — that is the vacuity above.
// Harden it instead by making the >=2 direction deterministic: a 2-party
// barrier (see barrierFinalizer) between two of the heavy rules turns "pool
// narrower than declared" into a barrier timeout instead of a timing read.
func TestRunBoundsHeavyFinalizersToTwo(t *testing.T) {
	const n = 4
	p := &peakTracker{}
	rls := make([]*config.Rule, 0, n)
	for i := range n {
		rls = append(rls, mustRule(t, fmt.Sprintf("heavy-%d", i), finding.SeverityError, []string{"**"},
			&heavyFinalizer{p: p, hold: 50 * time.Millisecond, id: fmt.Sprint(i)}))
	}

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), n)
	if err != nil {
		t.Fatal(err)
	}
	assertHeavyAllRan(t, got, n)

	if got := p.max(); got != 2 {
		t.Fatalf("peak concurrent heavy finalizers = %d, want exactly 2: more breaks the per-process "+
			"memory bound (N analyzers at once is N times the footprint, #67); fewer means the heavy "+
			"pool is narrower than its declared width and serialises what the bound deliberately allows", got)
	}
}

// TestRunWorkersOneSerialisesHeavyFinalizers pins the operator escape hatch:
// before the pool split, --workers 1 made phase 2 fully serial, and the
// memory-starved machine is #67's own scenario. The heavy width must therefore
// be min(workers, heavyFinalizerWorkers) — a constant width-2 pool that
// ignores --workers would fork two analyzer-class processes on exactly the
// machine the operator throttled to protect.
func TestRunWorkersOneSerialisesHeavyFinalizers(t *testing.T) {
	p := &peakTracker{}
	rls := []*config.Rule{
		mustRule(t, "heavy-0", finding.SeverityError, []string{"**"},
			&heavyFinalizer{p: p, hold: 50 * time.Millisecond, id: "0"}),
		mustRule(t, "heavy-1", finding.SeverityError, []string{"**"},
			&heavyFinalizer{p: p, hold: 50 * time.Millisecond, id: "1"}),
	}

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), 1)
	if err != nil {
		t.Fatal(err)
	}
	assertHeavyAllRan(t, got, 2)
	if got := p.max(); got != 1 {
		t.Fatalf("peak concurrent heavy finalizers = %d, want 1 under --workers 1: the operator's "+
			"concurrency throttle must bound the heavy pool too, never be exceeded by it (#67)", got)
	}
}

// heavyErrFinalizer errors from the HEAVY pool after a short delay — long
// enough that a fast erroring peer has already lost the wall-clock race, so
// declaration order (not pool or wall time) must decide which error surfaces.
type heavyErrFinalizer struct {
	err   error
	delay time.Duration
}

func (*heavyErrFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (*heavyErrFinalizer) Cost() rules.Cost                            { return rules.CostHeavy }
func (h *heavyErrFinalizer) FinalizeErr(rules.FinalizeContext) ([]rules.Match, error) {
	time.Sleep(h.delay)
	return nil, h.err
}

// TestRunFinalizerErrorIsDeterministicAcrossPools extends the determinism pin
// across the cost partition: TestRunFinalizerErrorIsDeterministic races two
// FAST finalizers inside one pool, so a regression that gave each pool its own
// error variable (merged last-wins) would slip past it. Here the first-declared
// rule errors in the heavy pool and LATER in wall time than its fast peer;
// declaration order must still win.
func TestRunFinalizerErrorIsDeterministicAcrossPools(t *testing.T) {
	fset := memFileSet(map[string]string{"a.txt": "x"})
	for i := range 25 {
		rls := []*config.Rule{
			mustRule(t, "heavy-first-failing", finding.SeverityError, []string{"**"},
				&heavyErrFinalizer{err: errFake, delay: 10 * time.Millisecond}),
			mustRule(t, "fast-late-failing", finding.SeverityError, []string{"**"},
				&fakeErrFinalizer{err: errFake}),
		}
		_, err := engine.Run(rls, fset, 4)
		if err == nil {
			t.Fatalf("run %d: expected an engine error", i)
		}
		if !strings.Contains(err.Error(), "heavy-first-failing") {
			t.Fatalf("run %d: error %q must name the first rule in declaration order, "+
				"whichever pool it ran in and whenever its error landed", i, err)
		}
	}
}

// TestRunKeepsFastFinalizersConcurrentAlongsideHeavyOnes is the other half, and
// the half a careless fix silently loses: serialising phase 2 WHOLESALE also
// satisfies the test above, while undoing the parallelism
// finalizer_pool_test.go exists to protect. The barrier finalizers here can only
// complete if all four are inside Finalize together, and they must manage it
// while a heavy rule is occupying the heavy pool — which is only possible if
// the two pools run concurrently rather than one after the other.
//
// This guard passes both before and after #67's fix, so it was verified
// load-bearing by falsification: replacing the two pools with a single serial
// one turns it red (peer never enters Finalize), while the test above stays
// green. A guard that cannot fail is worse than no guard.
func TestRunKeepsFastFinalizersConcurrentAlongsideHeavyOnes(t *testing.T) {
	const n = 4
	b := newBarrier(n)
	p := &peakTracker{}

	rls := make([]*config.Rule, 0, n+3)
	// Heavy rules first in declaration order: if the pools were sequential
	// rather than concurrent, these would run to completion before any barrier
	// finalizer started, and the barrier would time out. Three of them so the
	// width-2 bound below is load-bearing — with two it could never trip.
	for i := range 3 {
		rls = append(rls, mustRule(t, fmt.Sprintf("heavy-%d", i), finding.SeverityError, []string{"**"},
			&heavyFinalizer{p: p, hold: 50 * time.Millisecond, id: fmt.Sprint(i)}))
	}
	for i := range n {
		rls = append(rls, mustRule(t, fmt.Sprintf("barrier-%d", i), finding.SeverityError, []string{"**"},
			&barrierFinalizer{b: b, timeout: 2 * time.Second}))
	}

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), n)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range got {
		if strings.Contains(fd.Message, "ran this finalizer alone") {
			t.Fatalf("%s: %s — bounding HEAVY finalizers must not cost FAST ones their pool; "+
				"the two pools run concurrently (#67)", fd.RuleID, fd.Message)
		}
	}
	assertHeavyAllRan(t, got, 3)
	if got := p.max(); got > 2 {
		t.Fatalf("peak concurrent heavy finalizers = %d, want <= 2 even in a mixed run", got)
	}
}

// wideHeavyFinalizer is CostHeavy (hooks still skip it) but not ProcessBound
// — the go run / bash command class. Four of them at --workers 4 must overlap
// all four: that is the 17-minute `check --lane ci` regression. If they
// still sit in the width-2 analyzer pool, peak is 2 and the validating
// port's guardrail suite serialises ~230 detectors behind Dart.
type wideHeavyFinalizer struct {
	p    *peakTracker
	hold time.Duration
	id   string
}

func (*wideHeavyFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (*wideHeavyFinalizer) Cost() rules.Cost                            { return rules.CostHeavy }
func (*wideHeavyFinalizer) ProcessBound() bool                          { return false }
func (f *wideHeavyFinalizer) Finalize() []rules.Match {
	f.p.enter()
	time.Sleep(f.hold)
	f.p.leave()
	return []rules.Match{{Message: "heavy finalizer ran: " + f.id}}
}

// TestRunWideHeavyFinalizersUseFullWorkerWidth is the cheap-command half of
// #67's pool split. Unbounded Dart is still forbidden
// (TestRunBoundsHeavyFinalizersToTwo). Serialising go run with Dart is the
// other defect: peak must be exactly --workers, not 2.
func TestRunWideHeavyFinalizersUseFullWorkerWidth(t *testing.T) {
	const n = 4
	p := &peakTracker{}
	rls := make([]*config.Rule, 0, n)
	for i := range n {
		rls = append(rls, mustRule(t, fmt.Sprintf("wide-%d", i), finding.SeverityError, []string{"**"},
			&wideHeavyFinalizer{p: p, hold: 50 * time.Millisecond, id: fmt.Sprint(i)}))
	}

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), n)
	if err != nil {
		t.Fatal(err)
	}
	assertHeavyAllRan(t, got, n)
	if got := p.max(); got != n {
		t.Fatalf("peak concurrent wide-heavy finalizers = %d, want exactly %d: "+
			"CostHeavy go/bash command rules must use the full worker width, "+
			"not the Dart analyzer cap", got, n)
	}
}

// TestRunWideAndBoundHeaviesShareWorkers pins the oversubscription case:
// 2 Dart + GOMAXPROCS go run oversubscribed a 4 vCPU runner and the vacuity
// census missed its 10s budget. Combined subprocesses must stay at --workers;
// bound still never exceeds 2.
func TestRunWideAndBoundHeaviesShareWorkers(t *testing.T) {
	const workers = 4
	boundP, wideP := &peakTracker{}, &peakTracker{}
	rls := make([]*config.Rule, 0, 7)
	for i := range 3 {
		rls = append(rls, mustRule(t, fmt.Sprintf("bound-%d", i), finding.SeverityError, []string{"**"},
			&heavyFinalizer{p: boundP, hold: 50 * time.Millisecond, id: "b" + fmt.Sprint(i)}))
	}
	for i := range 4 {
		rls = append(rls, mustRule(t, fmt.Sprintf("wide-%d", i), finding.SeverityError, []string{"**"},
			&wideHeavyFinalizer{p: wideP, hold: 50 * time.Millisecond, id: "w" + fmt.Sprint(i)}))
	}

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), workers)
	if err != nil {
		t.Fatal(err)
	}
	assertHeavyAllRan(t, got, 7)
	if p := boundP.max(); p > 2 {
		t.Fatalf("peak bound heavies = %d, want <= 2", p)
	}
	if p := wideP.max(); p > 2 {
		t.Fatalf("peak wide heavies = %d, want <= 2 when Dart is also running (workers=4)", p)
	}
	if boundP.max() < 1 || wideP.max() < 1 {
		t.Fatal("both pools must run; a skip would look like a clean cap")
	}
}
