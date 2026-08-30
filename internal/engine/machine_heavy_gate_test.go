// machine_heavy_gate_test.go — #67 bounds analyzer-class heavy finalizers to
// K within ONE engine.Run, and heavy_finalizer_pool_test.go pins that. #81 is
// that the memory does not live in a run, it lives in the machine: the
// operator runs several agent sessions in parallel, each its own checkout,
// each invoking formwork. Every one of those processes honours K and the
// machine still dies, because five processes forking one 3.2-8.6 GB analyzer
// each is 15-43 GB. #67's acceptance criterion is satisfied throughout — it is
// a per-run measurement, and a green metric beside a live failure is the shape
// this engine's review culture exists to catch.
//
// The contract here has three halves, and dropping any one of them ships a
// governor that reads as working:
//
//  1. the bound holds across acquirers that share NO in-process semaphore
//     (TestHeavyGateExcludesIndependentAcquirers, and across two concurrent
//     engine.Run calls in TestRunBoundsProcessBoundHeaviesAcrossIndependentRuns);
//  2. a holder that DIES leaves nothing behind — a stale lock that blocks every
//     future run forever is worse than the hang this fixes
//     (TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath);
//  3. when the mechanism cannot work at all the run proceeds UNBOUNDED and
//     says so on stderr, rather than refusing
//     (TestRunProceedsWithDisclosureWhenMachineGateIsUnusable). This is
//     deliberately not the repo's usual fail-closed default: the gate is a
//     resource governor, not a check, so failing closed would turn a lock-file
//     problem into a false CI failure on a repo with no violations.
package engine_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
)

// TestMain points the whole engine test binary at a throwaway slot directory.
// Without it every test in this package that declares a CostHeavy finalizer
// would take real locks in the operator's cache directory, and two checkouts
// running `make test PKG=./internal/engine` at once would serialise against
// each other.
func TestMain(m *testing.M) {
	if os.Getenv(gateHolderDirEnv) != "" {
		// The re-exec'd holder process below. It is handed the directory it
		// must lock, so it needs no throwaway one — and it is SIGKILLed while
		// holding its slots, so it never returns from m.Run to clean anything
		// up. Creating a temp directory here therefore orphaned one per
		// invocation of the test that spawns it: per `make test`, per CI job,
		// unbounded. TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath
		// points the child's TMPDIR at a directory it owns and counts.
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "formwork-engine-heavy-gate")
	if err != nil {
		fmt.Fprintln(os.Stderr, "heavy-gate test setup:", err)
		os.Exit(2)
	}
	engine.SetHeavyGateDir(dir) // deliberately not restored: it must outlive every test here
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// warnRecorder collects the gate's disclosures. Concurrency-safe because the
// gate is acquired from every finalizer goroutine at once.
type warnRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnRecorder) warn(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, fmt.Sprintf(format, args...))
}

func (w *warnRecorder) all() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.msgs...)
}

// TestRunBoundsProcessBoundHeaviesAcrossIndependentRuns is #81's acceptance
// criterion, at the altitude the issue says the per-run one is measured at:
// two independent engine.Run invocations, each with its own in-process pool,
// each honouring #67's width — and the peak that matters is the one taken
// ACROSS them. Two runs of two analyzer-class rules at --workers 2 is peak 4
// unbounded, which downstream is four analyzers and ~20 GB from two processes
// that each pass #67.
//
// The two Runs stand in for two formwork processes. They coordinate through
// nothing but the slot files (TestHeavyGateExcludesIndependentAcquirers pins
// that the primitive really does exclude at file-descriptor granularity, which
// is what makes this a valid stand-in), and
// TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath then proves the
// same across a real exec'd process.
func TestRunBoundsProcessBoundHeaviesAcrossIndependentRuns(t *testing.T) {
	defer engine.SetHeavyGateDir(t.TempDir())()
	p := &peakTracker{}

	const runs, perRun = 2, 2
	sets := make([][]*config.Rule, runs)
	for r := range runs {
		for i := range perRun {
			sets[r] = append(sets[r], mustRule(t, fmt.Sprintf("heavy-%d-%d", r, i),
				finding.SeverityError, []string{"**"},
				// The hold has to outlast the peer run's dispatch, or an
				// unbounded engine could interleave four analyzers so fast
				// that a peak of 2 proves nothing.
				&heavyFinalizer{p: p, hold: 200 * time.Millisecond, id: fmt.Sprintf("%d-%d", r, i)}))
		}
	}

	var (
		wg   sync.WaitGroup
		got  = make([][]finding.Finding, runs)
		errs = make([]error, runs)
	)
	for r := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[r], errs[r] = engine.Run(sets[r], memFileSet(map[string]string{"a.txt": "x"}), perRun)
		}()
	}
	wg.Wait()

	for r := range runs {
		if errs[r] != nil {
			t.Fatalf("run %d: %v", r, errs[r])
		}
		assertHeavyAllRan(t, got[r], perRun)
	}
	if peak := p.max(); peak > engine.HeavyFinalizerWorkers {
		t.Fatalf("peak concurrent analyzer-class finalizers across %d independent runs = %d, want <= %d: "+
			"each run honouring the per-process bound is exactly the state #81 reports — every run passes "+
			"#67's criterion while the machine hangs, because N processes forking one 3.2-8.6 GB analyzer "+
			"each is N times the footprint", runs, peak, engine.HeavyFinalizerWorkers)
	}
	if peak := p.max(); peak < 1 {
		t.Fatal("no analyzer-class finalizer ever entered the guarded region: a bound over work that " +
			"never happened is not a bound, it is a silent skip")
	}
}

// TestHeavyGateExcludesIndependentAcquirers drives the primitive directly.
// Six acquirers, each holding its OWN gate value over one directory, so the
// only thing that can bound them is the lock on disk — no shared mutex, no
// shared channel, no shared pool.
//
// The exactly-K assertion is load-bearing in both directions. Over K is the
// memory bound broken. Under K is the failure mode a POSIX fcntl (F_SETLK)
// implementation would have here in reverse: fcntl locks are held per PROCESS,
// so six same-process acquirers would all "succeed" and the peak would be 6.
// flock(2) locks the open file description instead, so a second open() of the
// same slot conflicts even from the same process — which is why this test can
// stand in for separate processes at all.
func TestHeavyGateExcludesIndependentAcquirers(t *testing.T) {
	dir := t.TempDir()
	const acquirers = 6
	p := &peakTracker{}
	var w warnRecorder

	var wg sync.WaitGroup
	for range acquirers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := engine.NewTestGate(dir, engine.HeavyFinalizerWorkers, 30*time.Second, 5*time.Millisecond, w.warn)
			release := g.Acquire()
			defer release()
			p.enter()
			time.Sleep(60 * time.Millisecond)
			p.leave()
		}()
	}
	wg.Wait()

	if msgs := w.all(); len(msgs) != 0 {
		t.Fatalf("gate disclosed a fail-open it should not have taken: %q — the slots were free within "+
			"the deadline, so every acquirer had to get a real one", msgs)
	}
	if peak := p.max(); peak != engine.HeavyFinalizerWorkers {
		t.Fatalf("peak concurrent holders = %d, want exactly %d: more means the machine-wide slots do not "+
			"exclude each other; fewer means the gate serialises past its declared width and an operator "+
			"pays wall time the bound was sized not to cost", peak, engine.HeavyFinalizerWorkers)
	}
}

const gateHolderDirEnv = "FORMWORK_TEST_HEAVY_GATE_DIR"

// TestHeavyGateHolderHelper is not a test. It is the other PROCESS: re-exec'd
// by the test below, it takes every machine-wide slot, announces that it has
// them, and then waits to be killed.
func TestHeavyGateHolderHelper(t *testing.T) {
	dir := os.Getenv(gateHolderDirEnv)
	if dir == "" {
		t.Skip("helper process only; driven by TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath")
	}
	var w warnRecorder
	for range engine.HeavyFinalizerWorkers {
		g := engine.NewTestGate(dir, engine.HeavyFinalizerWorkers, 30*time.Second, 5*time.Millisecond, w.warn)
		// Deliberately never released: the point is to die holding them.
		_ = g.Acquire()
	}
	if msgs := w.all(); len(msgs) != 0 {
		fmt.Println("HELPER-FAILED-TO-HOLD", msgs)
		return
	}
	fmt.Println("HELD")
	os.Stdout.Sync()
	time.Sleep(60 * time.Second)
}

// TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath is the whole of
// #81 in one test, and the half that most needs to be true is the second.
//
// Part 1: while a genuinely separate process holds every slot, this process
// must NOT get one — that is the cross-process bound.
//
// Part 2: SIGKILL that holder and this process must get a slot immediately,
// with no disclosure. A lock scheme built on the EXISTENCE of a file, a PID
// file, or a mkdir would pass part 1 and fail here: the killed holder never
// runs a cleanup path, so its marker outlives it and every future formwork run
// on that machine either blocks or falls open forever. flock(2) is chosen for
// exactly this — the kernel drops the lock when the holder's last descriptor
// closes, which happens on any death including SIGKILL, so there is no stale
// state to reap and no reaper to get wrong.
func TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath(t *testing.T) {
	dir := t.TempDir()

	// The child gets a TMPDIR this test owns, so what it leaves behind is
	// countable. It is killed mid-sleep and runs no cleanup path of any kind,
	// so anything its TestMain creates outlives it forever.
	childTmp := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHeavyGateHolderHelper$", "-test.v=false")
	cmd.Env = append(os.Environ(), gateHolderDirEnv+"="+dir, "TMPDIR="+childTmp)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	held := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line == "HELD" || strings.HasPrefix(line, "HELPER-FAILED") {
				held <- line
				return
			}
		}
		held <- ""
	}()
	select {
	case line := <-held:
		if line != "HELD" {
			t.Fatalf("holder process never took the slots (%q): the cross-process half of this test "+
				"cannot mean anything until it does", line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the holder process to take the slots")
	}

	var blocked warnRecorder
	g := engine.NewTestGate(dir, engine.HeavyFinalizerWorkers, 300*time.Millisecond, 10*time.Millisecond, blocked.warn)
	release := g.Acquire()
	release()
	if msgs := blocked.all(); len(msgs) == 0 {
		t.Fatal("acquired a machine-wide slot while another PROCESS held every one of them: the bound is " +
			"per-process again, which is #81 exactly")
	} else if !strings.Contains(strings.Join(msgs, "\n"), "unbounded") {
		t.Fatalf("fail-open disclosure %q must say the run is proceeding UNBOUNDED — an operator who "+
			"cannot see the governor gave up is back to diagnosing a hang with no evidence", msgs)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()

	// A killed holder must leave NOTHING behind — that is the whole reason the
	// gate is built on flock rather than on a marker file, and it has to be
	// true of the test harness around it too, or every `make test` and every CI
	// job orphans a directory in the operator's TMPDIR forever.
	if leaked, err := os.ReadDir(childTmp); err != nil {
		t.Fatal(err)
	} else if len(leaked) != 0 {
		names := make([]string, 0, len(leaked))
		for _, e := range leaked {
			names = append(names, e.Name())
		}
		t.Fatalf("the SIGKILLed holder process left %d entries in its TMPDIR (%v): it is killed while "+
			"holding slots and never returns from m.Run, so anything TestMain creates in the child is "+
			"orphaned once per run of this test, unbounded", len(leaked), names)
	}

	var afterDeath warnRecorder
	g2 := engine.NewTestGate(dir, engine.HeavyFinalizerWorkers, 5*time.Second, 10*time.Millisecond, afterDeath.warn)
	start := time.Now()
	release2 := g2.Acquire()
	defer release2()
	if msgs := afterDeath.all(); len(msgs) != 0 {
		t.Fatalf("after the holder was SIGKILLed the gate still could not hand out a slot (%q, waited %s): "+
			"the holder's lock outlived it, so every future run on this machine is blocked by a corpse — "+
			"which is worse than the hang this bound exists to prevent", msgs, time.Since(start))
	}
}

// TestRunProceedsWithDisclosureWhenMachineGateIsUnusable pins the degradation
// direction #81 asks for and this repo otherwise forbids: when the slot
// directory cannot even be created, the run must complete normally and SAY so,
// not refuse. Refusing would convert an unwritable cache directory into a
// false violation on a clean tree.
func TestRunProceedsWithDisclosureWhenMachineGateIsUnusable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Restored on the way out. The blocker is a t.TempDir() that is removed
	// when this test ends, so leaving the gate pointed at it does not fail
	// loudly for a later test — MkdirAll on the freed path SUCCEEDS and the
	// gate silently relocates outside the directory TestMain manages.
	defer engine.SetHeavyGateDir(filepath.Join(blocker, "slots"))()
	var w warnRecorder
	defer engine.SetHeavyGateWarn(w.warn)()

	const n = 3
	p := &peakTracker{}
	rls := make([]*config.Rule, 0, n)
	for i := range n {
		rls = append(rls, mustRule(t, fmt.Sprintf("heavy-%d", i), finding.SeverityError, []string{"**"},
			&heavyFinalizer{p: p, hold: 20 * time.Millisecond, id: fmt.Sprint(i)}))
	}

	got, err := engine.Run(rls, memFileSet(map[string]string{"a.txt": "x"}), n)
	if err != nil {
		t.Fatalf("an unusable machine-wide gate must not fail the run: %v", err)
	}
	assertHeavyAllRan(t, got, n)

	msgs := strings.Join(w.all(), "\n")
	if msgs == "" {
		t.Fatal("the gate fell open in silence: an operator whose runs are no longer bounded machine-wide " +
			"has to be able to see it, or the per-process bound reads as the machine-wide one (#81)")
	}
	if !strings.Contains(msgs, "unbounded") {
		t.Fatalf("disclosure %q must name what is now unbounded", msgs)
	}
	if strings.Count(msgs, "unbounded") != 1 {
		t.Fatalf("the gate disclosed once per finalizer (%d rules, %q): a per-rule repeat buries the "+
			"line it is trying to make visible", n, msgs)
	}
}
