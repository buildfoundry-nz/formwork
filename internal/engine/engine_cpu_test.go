package engine_test

import (
	"maps"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type workFinalizer struct {
	fakeChecker
	work func()
}

func (f *workFinalizer) Finalize() []rules.Match {
	f.work()
	return nil
}

var cpuLoadSink atomic.Uint64

// startCPULoad returns once every load goroutine has spun at least once. One
// runtime.Gosched did not guarantee the load was on a P when the measured
// rule started, so on a darwin runner the loaded wall barely moved and the
// contention test could prove nothing.
func startCPULoad() func() {
	stop := make(chan struct{})
	var wg, running sync.WaitGroup
	for range 4 * runtime.GOMAXPROCS(0) {
		wg.Add(1)
		running.Add(1)
		go func() {
			defer wg.Done()
			x := uint64(1)
			first := true
			for {
				for range 1024 {
					x = x*1664525 + 1013904223
				}
				cpuLoadSink.Store(x)
				if first {
					running.Done()
					first = false
				}
				select {
				case <-stop:
					return
				default:
				}
			}
		}()
	}
	running.Wait()
	return func() {
		close(stop)
		wg.Wait()
	}
}

func measureCPUUnderLoad(t *testing.T, r *config.Rule, loaded bool) engine.Timings {
	t.Helper()
	fset := memFileSet(map[string]string{"a.txt": "x"})
	var stop func()
	if loaded {
		stop = startCPULoad()
	}
	_, timings, err := engine.RunTimed([]*config.Rule{r}, fset, 1, nil)
	if stop != nil {
		stop()
	}
	if err != nil {
		t.Fatal(err)
	}
	return timings
}

func assertCPUKeysMatchWall(t *testing.T, timings engine.Timings) {
	t.Helper()
	wallKeys := slices.Sorted(maps.Keys(timings.Wall))
	cpuKeys := slices.Sorted(maps.Keys(timings.CPU))
	if !slices.Equal(wallKeys, cpuKeys) {
		t.Fatalf("CPU keys %v != wall keys %v", cpuKeys, wallKeys)
	}
}

func medianDuration(values []time.Duration) time.Duration {
	slices.Sort(values)
	return values[len(values)/2]
}

func TestRunTimedCPUIsStableUnderContention(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(2)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	const iterations = 100_000_000
	for _, tc := range []struct {
		name    string
		include []string
		checker rules.Checker
	}{
		{
			name:    "phase 1",
			include: []string{"**/*.txt"},
			checker: &fakeChecker{match: func(*scan.File) []rules.Match {
				cpuBenchSink = spinForCPU(iterations)
				return nil
			}},
		},
		{
			name:    "finalizer",
			include: []string{"not-in-scope/**"},
			checker: &workFinalizer{work: func() { cpuBenchSink = spinForCPU(iterations) }},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newRule := func() *config.Rule {
				return mustRule(t, "spin", finding.SeverityError, tc.include, tc.checker)
			}
			var quietCPU, loadedCPU, quietWall, loadedWall []time.Duration
			for range 3 {
				quiet := measureCPUUnderLoad(t, newRule(), false)
				loaded := measureCPUUnderLoad(t, newRule(), true)
				assertCPUKeysMatchWall(t, quiet)
				assertCPUKeysMatchWall(t, loaded)
				quietCPU = append(quietCPU, quiet.CPU["spin"])
				loadedCPU = append(loadedCPU, loaded.CPU["spin"])
				quietWall = append(quietWall, quiet.Wall["spin"])
				loadedWall = append(loadedWall, loaded.Wall["spin"])
			}
			quietCPUMedian, loadedCPUMedian := medianDuration(quietCPU), medianDuration(loadedCPU)
			if quietCPUMedian <= 0 || loadedCPUMedian < quietCPUMedian*75/100 || loadedCPUMedian > quietCPUMedian*125/100 {
				t.Fatalf("CPU moved under load: quiet median=%s loaded median=%s (want within 25%%; samples quiet=%v loaded=%v)", quietCPUMedian, loadedCPUMedian, quietCPU, loadedCPU)
			}
			quietWallMedian, loadedWallMedian := medianDuration(quietWall), medianDuration(loadedWall)
			if loadedWallMedian <= quietWallMedian*3/2 {
				t.Fatalf("load did not stretch wall enough to prove the contrast: quiet median=%s loaded median=%s", quietWallMedian, loadedWallMedian)
			}
		})
	}
}

func cpuRuleChecker(t *testing.T, kind, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup(kind)
	if !ok {
		t.Fatalf("rule type %q is not registered", kind)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	checker, err := factory(doc.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	return checker
}

func TestRunTimedChargesRegexp2Work(t *testing.T) {
	checker := cpuRuleChecker(t, "forbidden-pattern", "pattern: '(?<=a)b'\nsyntax: regexp2\n")
	r := mustRule(t, "regexp2", finding.SeverityError, []string{"**/*.txt"}, checker)
	fset := memFileSet(map[string]string{"a.txt": strings.Repeat("a", 10_000) + "b"})
	_, timings, err := engine.RunTimed([]*config.Rule{r}, fset, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertCPUKeysMatchWall(t, timings)
	if timings.CPU["regexp2"] <= 0 {
		t.Fatalf("regexp2 work escaped per-rule CPU accounting: %+v", timings)
	}
	offThread := rules.OffThreadCPUOf(checker)
	if offThread <= 0 || timings.CPU["regexp2"] < offThread {
		t.Fatalf("engine CPU %s does not include the checker's off-thread charge %s", timings.CPU["regexp2"], offThread)
	}
}

var gcSink []byte

func TestRunTimedCPUTotalIncludesChildrenAndRuntimeResidual(t *testing.T) {
	phase1 := mustRule(t, "phase1", finding.SeverityError, []string{"**/*.txt"}, &fakeChecker{match: func(*scan.File) []rules.Match {
		cpuBenchSink = spinForCPU(2_000_000)
		return nil
	}})
	allocator := &workFinalizer{work: func() {
		for range 1_000_000 {
			block := make([]byte, 256)
			block[0] = 1
			gcSink = block
		}
		runtime.GC()
	}}
	gcRule := mustRule(t, "gc", finding.SeverityError, []string{"not-in-scope/**"}, allocator)
	command := cpuRuleChecker(t, "command", "cmd: [sh, -c, "+strconv.Quote("i=0; while [ $i -lt 20000 ]; do i=$((i+1)); done")+"]\n")
	commandRule := mustRule(t, "command", finding.SeverityError, []string{"not-in-scope/**"}, command)
	fset := memFileSet(map[string]string{"a.txt": "x"})
	fset.Root = t.TempDir()
	_, timings, err := engine.RunTimed([]*config.Rule{phase1, gcRule, commandRule}, fset, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertCPUKeysMatchWall(t, timings)
	var sum time.Duration
	for _, d := range timings.CPU {
		sum += d
	}
	if timings.CPUTotal*100 < sum*99 {
		t.Fatalf("cpu_total %s is below the per-rule sum %s beyond clock tolerance", timings.CPUTotal, sum)
	}
	if residual := timings.CPUTotal - sum; residual <= 0 {
		t.Fatalf("GC and other unattributed runtime work left no residual: total=%s sum=%s", timings.CPUTotal, sum)
	}
}
