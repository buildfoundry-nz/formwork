// finalizer_pool_test.go — phase 2 (finalizers) runs through a bounded worker
// pool, not one at a time. A consuming repo's lane can hold ~137 `command`
// rules that each shell out to an external script; run serially their latencies
// add up into minutes, while they are mutually independent (each reads the
// shared read-only FileSet and execs its own tool). Concurrency there must not
// cost either of the engine's contracts: findings still come back
// deterministically sorted by (rule id, path, line), and the engine error a
// failing finalizer produces is still the same one on every run.
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

// barrier releases only once n participants have arrived. arrive reports
// whether the release happened before the timeout, which turns "phase 2 ran
// serially" into a test FAILURE rather than a hung run.
type barrier struct {
	n     int
	mu    sync.Mutex
	count int
	ready chan struct{}
}

func newBarrier(n int) *barrier {
	return &barrier{n: n, ready: make(chan struct{})}
}

func (b *barrier) arrive(timeout time.Duration) bool {
	b.mu.Lock()
	b.count++
	if b.count == b.n {
		close(b.ready)
	}
	b.mu.Unlock()
	select {
	case <-b.ready:
		return true
	case <-time.After(timeout):
		return false
	}
}

// barrierFinalizer is a whole-run checker whose Finalize cannot complete unless
// every other barrier finalizer in the same run is inside Finalize at the same
// time. Serially, the first one waits on peers that will never start and times
// out; through a pool at least as wide as the rule count, they all arrive and
// release immediately.
type barrierFinalizer struct {
	b       *barrier
	timeout time.Duration
	matches []rules.Match
}

func (*barrierFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func (f *barrierFinalizer) Finalize() []rules.Match {
	if !f.b.arrive(f.timeout) {
		return []rules.Match{{Message: "phase 2 ran this finalizer alone — no peer entered Finalize concurrently"}}
	}
	return f.matches
}

func TestRunExecutesFinalizersConcurrently(t *testing.T) {
	const n = 4
	b := newBarrier(n)
	rls := make([]*config.Rule, 0, n)
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
			t.Fatalf("%s: %s — phase 2 must dispatch independent finalizers through a worker pool, "+
				"not one at a time", fd.RuleID, fd.Message)
		}
	}
}

// scopedFinalizer emits a fixed set of matches, so a run's whole finding list is
// known up front and any loss, duplication, or reordering is visible.
type scopedFinalizer struct{ matches []rules.Match }

func (*scopedFinalizer) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (f *scopedFinalizer) Finalize() []rules.Match                   { return f.matches }

// TestRunFinalizerFindingsAreDeterministicallySorted is the concurrency-safety
// half: several finalizers, each emitting several matches, merged from separate
// workers. Every run must yield the identical (rule id, path, line) sequence —
// no dropped merge, no ordering that depends on which worker finished first.
// Run under -race, it also pins that the merge is properly synchronised.
func TestRunFinalizerFindingsAreDeterministicallySorted(t *testing.T) {
	build := func() []*config.Rule {
		return []*config.Rule{
			mustRule(t, "zulu-rule", finding.SeverityError, []string{"**"}, &scopedFinalizer{matches: []rules.Match{
				{Path: "src/z.go", Line: 9, Message: "z late"},
				{Path: "src/a.go", Line: 2, Message: "z early"},
			}}),
			mustRule(t, "alpha-rule", finding.SeverityError, []string{"**"}, &scopedFinalizer{matches: []rules.Match{
				{Path: "src/m.go", Line: 7, Message: "a mid"},
				{Path: "src/m.go", Line: 1, Message: "a top"},
				{Message: "a scope-level"},
			}}),
			mustRule(t, "mike-rule", finding.SeverityError, []string{"**"}, &scopedFinalizer{matches: []rules.Match{
				{Path: "src/b.go", Line: 4, Message: "m only"},
			}}),
		}
	}
	want := []string{
		"alpha-rule::0",
		"alpha-rule:src/m.go:1",
		"alpha-rule:src/m.go:7",
		"mike-rule:src/b.go:4",
		"zulu-rule:src/a.go:2",
		"zulu-rule:src/z.go:9",
	}
	fset := memFileSet(map[string]string{"a.txt": "x", "b.txt": "x", "c.txt": "x"})
	for i := range 25 {
		got, err := engine.Run(build(), fset, 8)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(got))
		for _, fd := range got {
			keys = append(keys, fmt.Sprintf("%s:%s:%d", fd.RuleID, fd.Path, fd.Line))
		}
		if strings.Join(keys, "|") != strings.Join(want, "|") {
			t.Fatalf("run %d: findings = %v, want %v", i, keys, want)
		}
	}
}

// TestRunFinalizerErrorIsDeterministic pins the other contract a pool can cost:
// with two finalizers failing, the reported engine error must be the same one
// every run — the first in rule declaration order, as the serial pass reported
// it — not whichever worker happened to lose the race.
func TestRunFinalizerErrorIsDeterministic(t *testing.T) {
	fset := memFileSet(map[string]string{"a.txt": "x"})
	for i := range 25 {
		rls := []*config.Rule{
			mustRule(t, "first-failing", finding.SeverityError, []string{"**"}, &fakeErrFinalizer{err: errFake}),
			mustRule(t, "second-failing", finding.SeverityError, []string{"**"}, &fakeErrFinalizer{err: errFake}),
		}
		_, err := engine.Run(rls, fset, 4)
		if err == nil {
			t.Fatalf("run %d: expected an engine error", i)
		}
		if !strings.Contains(err.Error(), "first-failing") {
			t.Fatalf("run %d: error %q must name the first failing rule in declaration order", i, err)
		}
	}
}
