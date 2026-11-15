package command_test

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

func directChildCPU(t *testing.T, script string) time.Duration {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("direct child failed: %v: %s", err, out)
	}
	return cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
}

func commandChildCPU(t *testing.T, script string) time.Duration {
	t.Helper()
	c := build(t, "cmd: [sh, -c, "+strconv.Quote(script)+"]\n")
	if matches, err := finalize(t, c); err != nil || len(matches) != 0 {
		t.Fatalf("command failed: matches=%v err=%v", matches, err)
	}
	return rules.OffThreadCPUOf(c)
}

func busyLoop(n int) string {
	return fmt.Sprintf("i=0; while [ $i -lt %d ]; do i=$((i+1)); done", n)
}

func calibrateBusyLoop(t *testing.T) (int, time.Duration) {
	t.Helper()
	for n := 10_000; n <= 2_560_000; n *= 2 {
		cpu := directChildCPU(t, busyLoop(n))
		if cpu >= 300*time.Millisecond {
			return n, cpu
		}
	}
	t.Fatal("could not calibrate a shell busy loop to 300ms of CPU")
	return 0, 0
}

// pairedCPU runs script three times directly and three times through a command
// rule, interleaved, and returns the middle sample of each. rusage is time on
// a core, so the next run of one shell loop is not the same cost: one pair
// missed an 80% bar on linux at 420ms against 574ms (run 43141699089) and on
// darwin at 336ms against 431ms (run 43751745209), with the child fully
// charged. The middle of three is what the bar sees. A charge that was never
// added is still a miss against a loop calibrated to at least 300ms.
func pairedCPU(t *testing.T, script string) (direct, got time.Duration) {
	t.Helper()
	const samples = 3
	ds := make([]time.Duration, samples)
	gs := make([]time.Duration, samples)
	for i := 0; i < samples; i++ {
		ds[i] = directChildCPU(t, script)
		gs[i] = commandChildCPU(t, script)
	}
	return medianDuration(ds), medianDuration(gs)
}

func medianDuration(samples []time.Duration) time.Duration {
	s := append([]time.Duration(nil), samples...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func TestCommandSleepCPUIsNotWall(t *testing.T) {
	start := time.Now()
	cpu := commandChildCPU(t, "sleep 1")
	wall := time.Since(start)
	if wall < 900*time.Millisecond {
		t.Fatalf("sleep wall = %s, want about one second", wall)
	}
	if cpu >= 200*time.Millisecond {
		t.Fatalf("sleep child CPU = %s, want under 200ms (wall %s)", cpu, wall)
	}
}

func TestCommandBusyLoopCPUTracksDirectChild(t *testing.T) {
	n, _ := calibrateBusyLoop(t)
	direct, got := pairedCPU(t, busyLoop(n))
	if got < direct*80/100 {
		t.Fatalf("command CPU = %s, direct child = %s; want at least 80%%", got, direct)
	}
}

func TestCommandWaitedGrandchildCPUIsCharged(t *testing.T) {
	n, _ := calibrateBusyLoop(t)
	outer := "sh -c '" + busyLoop(n) + "'"
	direct, got := pairedCPU(t, outer)
	if got < direct*80/100 {
		t.Fatalf("waited grandchild CPU = %s, direct = %s; want at least 80%%", got, direct)
	}
}
