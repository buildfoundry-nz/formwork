package cputime_test

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/cputime"
)

var sink uint64

func spin(n uint64) uint64 {
	x := sink + 1
	for i := uint64(0); i < n; i++ {
		x = x*1664525 + 1013904223
	}
	return x
}

func TestThreadAndProcessClocks(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	threadBefore, threadOK := cputime.Thread()
	processBefore, processOK := cputime.Process()
	if threadOK != cputime.Supported || processOK != cputime.Supported {
		t.Fatalf("clock availability = (%v, %v), Supported = %v", threadOK, processOK, cputime.Supported)
	}
	sink = spin(2_000_000)
	threadAfter, _ := cputime.Thread()
	processAfter, _ := cputime.Process()
	if !cputime.Supported {
		if threadBefore != 0 || threadAfter != 0 || processBefore != 0 || processAfter != 0 {
			t.Fatal("unsupported clocks must return zero")
		}
		return
	}
	if threadAfter <= threadBefore {
		t.Fatalf("thread CPU did not advance: %s -> %s", threadBefore, threadAfter)
	}
	if processAfter < processBefore {
		t.Fatalf("process CPU went backwards: %s -> %s", processBefore, processAfter)
	}
}

func TestGoChargesItsMeter(t *testing.T) {
	var meter cputime.Meter
	got := <-cputime.Go(&meter, func() uint64 { return spin(5_000_000) })
	sink = got
	if cputime.Supported && meter.OffThreadCPU() <= 0 {
		t.Fatal("a spinning metered goroutine reported zero CPU")
	}
	if !cputime.Supported && meter.OffThreadCPU() != 0 {
		t.Fatalf("unsupported platform charged %s", meter.OffThreadCPU())
	}
}

func TestCombinedOutputChargesCleanExitAndExitError(t *testing.T) {
	if !cputime.Supported {
		t.Skip("process-state CPU assertion requires a supported host")
	}
	script := "i=0; while [ $i -lt 20000 ]; do i=$((i+1)); done"
	for _, tc := range []struct {
		name     string
		script   string
		exitCode int
	}{
		{name: "clean exit", script: script},
		{name: "exit error", script: script + "; exit 7", exitCode: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var meter cputime.Meter
			out, err := cputime.CombinedOutput(&meter, exec.Command("sh", "-c", tc.script))
			if tc.exitCode == 0 && err != nil {
				t.Fatalf("clean command failed: %v: %s", err, out)
			}
			if tc.exitCode != 0 {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != tc.exitCode {
					t.Fatalf("error = %v, want exit %d: %s", err, tc.exitCode, out)
				}
			}
			if meter.OffThreadCPU() <= 0 {
				t.Fatalf("busy child reported zero CPU; output: %s", out)
			}
		})
	}
}

func TestCombinedOutputStartFailureAddsNoCPU(t *testing.T) {
	var meter cputime.Meter
	_, err := cputime.CombinedOutput(&meter, exec.Command(fmt.Sprintf("formwork-missing-%d", time.Now().UnixNano())))
	if err == nil {
		t.Fatal("missing executable must fail")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("start failure is not an ExitError: %v", err)
	}
	if got := meter.OffThreadCPU(); got != 0 {
		t.Fatalf("a child that never started charged %s", got)
	}
}
