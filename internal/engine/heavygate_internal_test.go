// heavygate_internal_test.go — the parts of the machine-wide gate that are not
// reachable through a gate value: the no-progress deadline's arithmetic, the
// handover token it reads that arithmetic from, the per-slot degradation, and
// the directory override.
//
// The deadline is tested here rather than through two racing acquirers on
// purpose. A test that "proves" a moving queue never falls open by starting
// churning holders and watching a waiter succeed proves nothing when the
// waiter simply wins a slot in 20ms — it goes green against a total deadline
// too, which is exactly the tautological shape this repo's mutation rule is
// looking for. Feeding the observation vector directly is the only way the
// changing-queue half can be made to fail when the reset is removed.
package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// gateWarnRecorder collects the gate's disclosures. warnRecorder in
// machine_heavy_gate_test.go is the same idea one package over (engine_test);
// these tests are in-package because the deadline and the token are not part of
// the exported surface, and deliberately are not going to become part of it.
type gateWarnRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (w *gateWarnRecorder) warn(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, fmt.Sprintf(format, args...))
}

func (w *gateWarnRecorder) all() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.msgs...)
}

// TestGateProgressStallsOnlyWhenNoSlotChangesHands is the #81-review fix in one
// assertion pair. The deadline used to be a TOTAL wait, so honest saturation —
// five sessions' worth of analyzer rules queued through two slots, every one of
// them making progress — pushed every waiter past it and fell open into exactly
// the unbounded state the gate exists to prevent, while telling the operator a
// holder was "stopped or wedged". A no-progress deadline distinguishes the two
// cases the disclosure claims to distinguish.
func TestGateProgressStallsOnlyWhenNoSlotChangesHands(t *testing.T) {
	const wait = 10 * time.Second
	now := time.Unix(0, 0)

	// Half 1: a queue that is moving. Every sweep sees a slot that changed
	// hands, so no amount of elapsed time may trip the deadline.
	moving := &gateProgress{wait: wait}
	for i := range 100 {
		obs := []string{"held-by-someone", "handover-" + string(rune('a'+i%26)) + string(rune('a'+i/26))}
		now = now.Add(wait / 2)
		if moving.stalled(obs, now) {
			t.Fatalf("sweep %d: the gate gave up after %s while slots were still changing hands — "+
				"that is honest contention, not a wedged holder, and falling open there re-enters #81's "+
				"hang through the deadline while blaming a cause that is not true", i, wait/2*time.Duration(i+1))
		}
	}

	// Half 2: the case the deadline is actually for. The same observation
	// forever is a holder that is stopped or wedged; nothing will ever release.
	stuck := &gateProgress{wait: wait}
	same := []string{"frozen-0", "frozen-1"}
	base := time.Unix(0, 0)
	if stuck.stalled(same, base) {
		t.Fatal("the first sweep tripped the deadline: a gate that gives up before it has waited at all " +
			"is not a governor")
	}
	if stuck.stalled(same, base.Add(wait-time.Nanosecond)) {
		t.Fatal("tripped one nanosecond early")
	}
	if !stuck.stalled(same, base.Add(wait)) {
		t.Fatalf("no slot changed hands for %s and the gate still did not give up: a holder that is "+
			"SIGSTOPped or blocked on a filesystem that never answers would hang the run forever, which "+
			"is the one thing the deadline is here for", wait)
	}
}

// TestSlotTokenChangesOnEveryAcquisition pins the fact the deadline reads.
// "The queue is moving" is only observable to a waiter because whoever takes a
// slot stamps it; without the stamp, a fully saturated set of slots looks
// byte-for-byte identical to a set nobody has touched in an hour.
func TestSlotTokenChangesOnEveryAcquisition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slot-0.lock")

	read := func() string {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		return readSlotToken(f)
	}
	stamp := func() {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		writeSlotToken(f)
	}

	if got := read(); got != "" {
		t.Fatalf("a fresh slot file already carries a token %q", got)
	}
	stamp()
	first := read()
	if first == "" {
		t.Fatal("taking a slot left no token: a waiter cannot tell a handover from a wedged holder, so " +
			"every saturated queue reads as stalled and falls open")
	}
	stamp()
	if second := read(); second == first {
		t.Fatalf("two separate acquisitions of the same slot wrote the same token %q: a handover is "+
			"invisible and the no-progress deadline degrades back into a total one", second)
	}
}

// TestGateUsesTheSlotsItCanWhenOneIsUnusable — one slot that cannot be opened
// used to abandon the whole sweep and fall open across the machine. The
// reachable spelling is the documented TMPDIR fallback: /tmp/formwork-heavy-gate
// is a shared path, MkdirAll returns nil on someone else's 0o700 directory, and
// the first OpenFile inside it returns EACCES. Losing one slot is a TIGHTER
// bound; losing the gate is no bound at all.
func TestGateUsesTheSlotsItCanWhenOneIsUnusable(t *testing.T) {
	dir := t.TempDir()
	// A directory where a slot file belongs: O_RDWR on it is EISDIR, which is
	// the same shape as the EACCES case without needing a second uid.
	if err := os.Mkdir(filepath.Join(dir, "slot-0.lock"), 0o700); err != nil {
		t.Fatal(err)
	}

	var w gateWarnRecorder
	g := &heavyGate{dir: dir, slots: 2, wait: 200 * time.Millisecond, poll: 5 * time.Millisecond, warn: w.warn}
	release := g.acquire()
	defer release()

	if msgs := w.all(); len(msgs) != 0 {
		t.Fatalf("one unusable slot made the gate fall open machine-wide (%q): the other slot was there "+
			"to be taken, and a width-1 bound is tighter than the declared one, never weaker", msgs)
	}

	// And the whole sweep failing must still disclose — a gate that can reach
	// no slot at all is unbounded, and silence there is #81 again. This needs
	// its own directory: the gate above is still holding slot-1 in dir, and it
	// holds it as an ordinary FILE, so blocking that slot in place is not
	// something a second gate can arrange.
	allBlocked := t.TempDir()
	for i := range 2 {
		if err := os.Mkdir(filepath.Join(allBlocked, fmt.Sprintf("slot-%d.lock", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var w2 gateWarnRecorder
	g2 := &heavyGate{dir: allBlocked, slots: 2, wait: 200 * time.Millisecond, poll: 5 * time.Millisecond, warn: w2.warn}
	g2.acquire()()
	msgs := strings.Join(w2.all(), "\n")
	if !strings.Contains(msgs, "unbounded") {
		t.Fatalf("no slot was usable and the gate said %q: it must say the run is proceeding unbounded", msgs)
	}
	// And it must name the cause it actually has. Falling through to the
	// no-progress deadline would also print the word "unbounded" — after
	// spinning the whole wait out — while telling the operator a holder is
	// wedged, which is the mis-attribution the deadline half of this review
	// was about. Unreachable slots and a stopped holder are different repairs.
	if !strings.Contains(msgs, "cannot use any of") || strings.Contains(msgs, "stopped or wedged") {
		t.Fatalf("the gate blamed the wrong cause: %q. No slot could be OPENED, which is a permission or "+
			"path problem the operator fixes on disk; blaming a holder that is stopped or wedged sends "+
			"them looking for a process that does not exist", msgs)
	}
}

// TestHeavyGateDirHonoursTheEnvOverride — without it the gate directory is
// derived from HOME alone, so any test anywhere in the tree that declares a
// dart/flutter command rule takes locks in the operator's REAL cache and
// serialises against a `make test` in another worktree, and a container with a
// read-only HOME has no way to point it somewhere writable.
func TestHeavyGateDirHonoursTheEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "gate")
	t.Setenv(heavyGateDirEnv, want)
	if got := defaultHeavyGateDir(); got != want {
		t.Fatalf("defaultHeavyGateDir() = %q, want %q from %s", got, want, heavyGateDirEnv)
	}

	t.Setenv(heavyGateDirEnv, "")
	if got := defaultHeavyGateDir(); got == "" || got == want {
		t.Fatalf("an empty override must fall back to the cache directory, got %q", got)
	}
}

// TestSetHeavyGateDirRestoresTheDirectoryItReplaced — SetHeavyGateDir used to
// return nothing, so a test that repointed the gate left it repointed for every
// test that sorted after it. That is silent: the blocker directory a
// fail-open test aims at is a t.TempDir() that is then REMOVED, so the next
// MkdirAll on that path succeeds and the gate quietly relocates outside the
// throwaway directory TestMain manages, taking real locks under a resurrected
// path. Nothing downstream can notice — a peak-2 assertion is satisfied by the
// in-process pool alone, with the machine-wide half of the bound doing nothing.
func TestSetHeavyGateDirRestoresTheDirectoryItReplaced(t *testing.T) {
	before := heavyGateDir

	restore := SetHeavyGateDir(filepath.Join(t.TempDir(), "elsewhere"))
	if heavyGateDir == before {
		t.Fatal("SetHeavyGateDir did not repoint the gate at all")
	}
	restore()
	if heavyGateDir != before {
		t.Fatalf("gate directory after restore = %q, want the %q it replaced: a test that repoints the "+
			"gate and does not put it back disarms the machine-wide half of the bound for every test "+
			"that runs after it, and nothing downstream fails when it does", heavyGateDir, before)
	}

	// And the restored value must be USABLE, not merely equal to a string: the
	// hazard is a gate pointed somewhere it can no longer create slots, which
	// discloses once and then bounds nothing.
	var w gateWarnRecorder
	g := &heavyGate{dir: heavyGateDir, slots: 1, wait: 200 * time.Millisecond, poll: 5 * time.Millisecond, warn: w.warn}
	g.acquire()()
	if msgs := w.all(); len(msgs) != 0 {
		t.Fatalf("the restored gate directory %q cannot hand out a slot (%q)", heavyGateDir, msgs)
	}
}

// TestFlockBuildTagsCompileOnEveryClaimedPlatform — the flock file used to
// claim `//go:build unix` and the fallback `!unix`, which is wrong in the
// direction that leaves a platform with NO implementation compiled: solaris
// (no syscall.LOCK_EX) and aix (neither) satisfy `unix` and so were handed the
// flock file, and `go build` there failed outright. The fallback exists exactly
// to stop that, so the two constraints have to partition every GOOS by whether
// the syscall is really there, not by whether the platform is unix-like.
//
// The four platforms below are the boundary, one reason each:
//   - solaris: unix, and syscall.Flock resolves but LOCK_EX does not.
//   - aix:     unix, and neither resolves.
//   - illumos: unix, HAS both, and also sets the `solaris` build tag — which is
//     why the exclusion cannot be written `unix && !solaris`.
//   - windows: not unix at all, the case the fallback was written for.
func TestFlockBuildTagsCompileOnEveryClaimedPlatform(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		// Fail closed. A cross-build proof that skips itself when the toolchain
		// is missing reports a pass it did not earn, which is the one thing
		// this repo's engine exists to stop.
		t.Fatalf("cannot prove the build constraints without the go toolchain: %v", err)
	}
	for _, p := range []struct{ goos, goarch string }{
		{"solaris", "amd64"},
		{"aix", "ppc64"},
		{"illumos", "amd64"},
		{"windows", "amd64"},
	} {
		t.Run(p.goos, func(t *testing.T) {
			cmd := exec.Command(goBin, "build", ".")
			cmd.Env = append(os.Environ(), "GOOS="+p.goos, "GOARCH="+p.goarch, "CGO_ENABLED=0")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("GOOS=%s GOARCH=%s go build . failed (%v):\n%s\n"+
					"one of the two tryLockFile files claims this platform and cannot compile there, or "+
					"neither claims it — the build constraints must partition every GOOS by whether "+
					"flock(2) is in its syscall package", p.goos, p.goarch, err, out)
			}
		})
	}
}
