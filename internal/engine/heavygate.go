package engine

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// The machine-wide bound on analyzer-class heavy finalizers (#81).
//
// heavyFinalizerWorkers bounds them within ONE engine.Run. That is the right
// fix for one invocation and it is not the fix for the machine: the operator
// runs several agent sessions in parallel, each its own checkout, each
// invoking formwork. Every one of those processes honours the per-run width
// and the machine still dies — five processes forking one 3.2-8.6 GB analyzer
// each is 15-43 GB, with #67's acceptance criterion satisfied throughout,
// because it is measured per run.
//
// So the slot count here is heavyFinalizerWorkers itself, and that identity is
// the whole contract: the ceiling for the MACHINE is the ceiling that was
// measured for one process, so N parallel sessions cost what one run costs
// instead of N times it. Widening this without widening that (or the reverse)
// would mean the two bounds no longer describe the same resource.
//
// # Why flock(2), and not a lock file that exists
//
// A stale lock that blocks every future run forever is worse than the hang
// this prevents, and every scheme that marks a slot by CREATING something — a
// PID file, a mkdir, an O_EXCL sentinel — has that failure mode: the holder is
// killed, no cleanup path runs, and the marker outlives it. Reaping it means
// deciding whether a recorded PID is still the same program, which is a race
// with PID reuse, on top of a governor.
//
// flock(2) has no stale state to reap. The lock lives on the open file
// description, so the kernel drops it when the holder's last descriptor
// closes: on return, on panic, on SIGKILL, on a machine-wide OOM kill. A
// killed holder therefore leaves an ordinary empty file behind and the next
// acquirer takes it immediately — which is what
// TestMachineGateExcludesAnotherProcessAndSurvivesItsDeath asserts, by killing
// a real holder process.
//
// flock also locks per file DESCRIPTION rather than per process (unlike POSIX
// fcntl record locks), so two independent acquirers inside one process exclude
// each other exactly as two processes do.
//
// # Scope of the bound, stated honestly
//
//   - It is per user cache directory, not per kernel. Two sessions with
//     different HOME or XDG_CACHE_HOME coordinate through different slot files
//     and do not see each other. That is the multi-session case the issue
//     describes (one operator, many checkouts) and not the multi-user one.
//   - It governs only the analyzer class — CostHeavy checkers that
//     rules.ProcessBoundOf accepts, the same predicate heavyFinalizerWorkers
//     partitions on. It deliberately does not govern the cheap go/bash command
//     half of CostHeavy, for the wall-time reason recorded on
//     heavyFinalizerWorkers: putting that half behind a width-2 bound is what
//     produced a 17-minute `check --lane ci`, and a machine-wide version of
//     that bound would produce it across processes as well as within one.
//   - Waiters are not queued in arrival order, and NOTHING here bounds the
//     unfairness. The deadline below deliberately does not: it fires on a queue
//     that has stopped moving, so a waiter that keeps losing races to peers
//     that keep winning them waits as long as the machine's real backlog of
//     analyzer work takes to drain. That is the governor doing its job — the
//     wait is other formwork runs' actual work — and it is the honest reading
//     of what this mechanism can promise. It is not a fairness guarantee, and
//     an earlier comment here claimed the deadline provided one.
var heavyGateDir = defaultHeavyGateDir()

// heavyGateDirEnv repoints the slot directory. Two cases need it and neither is
// exotic: a container whose HOME is read-only has nowhere to put slots at all,
// and a test in any package that declares a dart/flutter command rule otherwise
// takes locks in the operator's real cache directory and serialises against a
// `make test` running in another worktree. It is a location, not a switch —
// there is still no way to turn the bound off, which stays deliberate.
const heavyGateDirEnv = "FORMWORK_HEAVY_GATE_DIR"

var (
	// heavyGateWait bounds how long one finalizer waits WITHOUT THE QUEUE
	// MOVING before it gives up and runs anyway. The distinction is the whole
	// value of the number, and getting it wrong is how the first cut of this
	// gate shipped a fail-open: as a TOTAL wait, five sessions' worth of
	// analyzer rules queued through two slots — perfectly healthy saturation,
	// ~25 invocations at a couple of minutes each — pushed every waiter past
	// ten minutes and let them all run unbounded, which is #81's hang re-entered
	// through the deadline. Worse, the disclosure told the operator a holder was
	// wedged, which in that case is false.
	//
	// As a no-progress deadline it fires on exactly the case the flock cannot
	// cover: a holder that is SIGSTOPped, or blocked on a filesystem that never
	// answers, and so never hands its slot on. flock covers the holder that
	// DIES; this covers the holder that will not move. A queue that IS moving
	// resets it on every handover and never trips it, however long the backlog.
	heavyGateWait = 10 * time.Minute
	heavyGatePoll = 25 * time.Millisecond
)

// heavyGateWarn is where the fail-open disclosure goes. It is os.Stderr
// directly rather than a writer threaded in from the caller: engine.Run has no
// writer parameter, and adding one reaches seven call sites across
// internal/cli, internal/meta and internal/fixturetest — a change to three
// packages this one does not otherwise touch. Deferred on purpose, and the
// deferral costs the operator nothing at a terminal or in CI logs, where the
// binary's stderr is the same stream cli.Run writes to. It does mean a test
// that captures cli.Run's stderr writer will not see these lines.
var heavyGateWarn = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "formwork: "+format+"\n", args...)
}

func defaultHeavyGateDir() string {
	if d := os.Getenv(heavyGateDirEnv); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "formwork", "heavy-gate")
	}
	// No HOME (or no XDG_CACHE_HOME on a stripped environment). TMPDIR is a
	// weaker anchor — a session that sets its own gets its own slots — but a
	// weaker bound beats none, and the alternative is to fail open silently.
	return filepath.Join(os.TempDir(), "formwork-heavy-gate")
}

// heavyGate is a semaphore whose slots are lock files, so its counterparties
// are other processes rather than other goroutines.
type heavyGate struct {
	dir   string
	slots int
	wait  time.Duration
	poll  time.Duration
	warn  func(string, ...any)
	once  sync.Once
}

// acquire returns the release for one machine-wide slot. It NEVER refuses:
// every failure path returns a no-op release, having disclosed once.
//
// This inverts the engine's usual contract on purpose, and the inversion is
// the reasoning most likely to be "corrected" later, so: everywhere else a
// broken mechanism must fail closed, because a rule that did not run reads as
// a pass. This is not a rule. It is a resource governor, and a governor that
// fails closed turns an unwritable cache directory into a red CI run on a tree
// with no violations. What it owes instead is honesty — hence the disclosure,
// which TestRunProceedsWithDisclosureWhenMachineGateIsUnusable pins in both
// directions (the run must succeed, AND it must say it is unbounded).
func (g *heavyGate) acquire() func() {
	if g == nil {
		return func() {}
	}
	if err := os.MkdirAll(g.dir, 0o700); err != nil {
		return g.failOpen("cannot create the machine-wide heavy-rule gate at %s (%v); "+
			"analyzer-class rules run unbounded across concurrent formwork runs", g.dir, err)
	}
	prog := &gateProgress{wait: g.wait}
	for {
		// One sweep. Every slot is tried, because a slot this process cannot
		// use is not a reason to abandon the ones it can: losing a slot makes
		// the bound TIGHTER than declared, while abandoning the sweep makes it
		// absent. The reachable spelling is the TMPDIR fallback above —
		// /tmp/formwork-heavy-gate is a shared path, MkdirAll returns nil on a
		// directory another user already owns, and the first OpenFile inside
		// their 0o700 directory is EACCES.
		obs := make([]string, g.slots)
		usable := 0
		var lastErr error
		var lastPath string
		for i := range g.slots {
			path := filepath.Join(g.dir, fmt.Sprintf("slot-%d.lock", i))
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				lastErr, lastPath = err, path
				continue
			}
			locked, err := tryLockFile(f)
			if err != nil {
				_ = f.Close()
				lastErr, lastPath = err, path
				continue
			}
			usable++
			if locked {
				// Stamp the slot before returning: this is what makes a
				// handover visible to everyone still waiting, and it is written
				// while holding the lock so no two writers race for it. It is
				// data, not state that grants exclusion — a holder that dies
				// leaves a stale token behind, and a stale token blocks nothing
				// and reaps nothing, unlike every scheme that marks a slot by
				// creating something.
				writeSlotToken(f)
				// Closing the descriptor is what releases the lock, so the
				// release and the cleanup are the same act and neither can be
				// forgotten without the other.
				return func() { _ = f.Close() }
			}
			obs[i] = readSlotToken(f)
			_ = f.Close()
		}
		if usable == 0 {
			return g.failOpen("cannot use any of the %d machine-wide heavy-rule slots under %s "+
				"(%s: %v); analyzer-class rules run unbounded across concurrent formwork runs",
				g.slots, g.dir, lastPath, lastErr)
		}
		if prog.stalled(obs, time.Now()) {
			return g.failOpen("no machine-wide heavy-rule slot under %s changed hands in %s, so a holder "+
				"is stopped or wedged rather than working; giving up and letting this run's analyzer-class "+
				"rules proceed unbounded (a holder that DIED would have released its slot immediately, and "+
				"a queue that is merely long does not trip this — the deadline measures handovers, not "+
				"total wait)", g.dir, g.wait)
		}
		time.Sleep(g.poll)
	}
}

// gateProgress turns g.wait from a total wait into a no-progress one. It is a
// separate type with an injected clock because the alternative — proving the
// moving-queue half by racing real holders against a real waiter — goes green
// against a total deadline too, the moment the waiter happens to win a slot
// quickly. heavygate_internal_test.go feeds it observations directly so the
// half that matters can actually fail.
type gateProgress struct {
	wait     time.Duration
	seen     []string
	deadline time.Time
	started  bool
}

// stalled records one sweep's observation of the slots and reports whether the
// queue has stopped moving for longer than wait. Any change in the observed
// tokens — a slot that changed hands, or one that became reachable or stopped
// being — is progress and restarts the clock.
func (p *gateProgress) stalled(obs []string, now time.Time) bool {
	if !p.started || !slices.Equal(obs, p.seen) {
		p.started = true
		p.seen = append(p.seen[:0], obs...)
		p.deadline = now.Add(p.wait)
		return false
	}
	return !now.Before(p.deadline)
}

// slotTokenSeq makes two acquisitions by this process distinguishable even
// inside one clock tick.
var slotTokenSeq atomic.Uint64

// writeSlotToken stamps a slot with a value unique to this acquisition, so a
// waiter reading the file can tell "someone else took this slot since I last
// looked" from "nobody has touched this in ten minutes". Errors are ignored on
// purpose: a slot that cannot be stamped costs the waiters one progress signal,
// and failing the acquisition over it would turn a governor into a gate.
func writeSlotToken(f *os.File) {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(buf[8:16], slotTokenSeq.Add(1))
	_, _ = f.WriteAt(buf[:], 0)
}

// readSlotToken reads whatever stamp a slot carries. An unreadable or unstamped
// slot reads as "", which is a stable value and therefore counts as no progress
// — the conservative direction, because it can only make the deadline fire on a
// queue that really has stopped.
func readSlotToken(f *os.File) string {
	var buf [16]byte
	n, _ := f.ReadAt(buf[:], 0)
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

// failOpen discloses once per gate — that is, once per engine.Run — and hands
// back a release that does nothing. Once, because the alternative is one line
// per analyzer rule, which on a corpus with dozens of them buries the line it
// is trying to make visible (TestRunProceedsWithDisclosureWhenMachineGateIsUnusable
// counts it).
func (g *heavyGate) failOpen(format string, args ...any) func() {
	g.once.Do(func() {
		warn := g.warn
		if warn == nil {
			warn = heavyGateWarn
		}
		warn(format, args...)
	})
	return func() {}
}
