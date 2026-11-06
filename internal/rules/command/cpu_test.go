package command_test

import (
	"fmt"
	"os/exec"
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
	n, direct := calibrateBusyLoop(t)
	got := commandChildCPU(t, busyLoop(n))
	if got < direct*80/100 {
		t.Fatalf("command CPU = %s, direct child = %s; want at least 80%%", got, direct)
	}
}

func TestCommandWaitedGrandchildCPUIsCharged(t *testing.T) {
	n, _ := calibrateBusyLoop(t)
	outer := "sh -c '" + busyLoop(n) + "'"
	direct := directChildCPU(t, outer)
	got := commandChildCPU(t, outer)
	if got < direct*80/100 {
		t.Fatalf("waited grandchild CPU = %s, direct = %s; want at least 80%%", got, direct)
	}
}
