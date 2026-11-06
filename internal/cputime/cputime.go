// Package cputime is the single seam through which rule code runs work off its
// calling thread while retaining per-rule CPU attribution.
package cputime

import (
	"os/exec"
	"runtime"
	"sync/atomic"
	"time"
)

// Meter accumulates user and system CPU consumed by goroutines and child
// processes started by one rule. Its zero value is ready for concurrent use.
type Meter struct {
	ns atomic.Int64
}

// Add charges d to the meter.
func (m *Meter) Add(d time.Duration) {
	m.ns.Add(int64(d))
}

// OffThreadCPU returns CPU charged by metered goroutines and child processes.
func (m *Meter) OffThreadCPU() time.Duration {
	return time.Duration(m.ns.Load())
}

// Go runs fn on a new goroutine and charges its thread CPU to m before sending
// fn's result. A nil meter is a programming error.
func Go[T any](m *Meter, fn func() T) <-chan T {
	if m == nil {
		panic("cputime.Go: nil meter")
	}
	ch := make(chan T, 1)
	go func() {
		runtime.LockOSThread()
		before, ok := Thread()
		v := fn()
		after, afterOK := Thread()
		runtime.UnlockOSThread()
		if ok && afterOK {
			m.Add(after - before)
		}
		ch <- v
	}()
	return ch
}

// CombinedOutput runs cmd and charges the child's user and system CPU whenever
// it started, including when it returned an *exec.ExitError.
func CombinedOutput(m *Meter, cmd *exec.Cmd) ([]byte, error) {
	if m == nil {
		panic("cputime.CombinedOutput: nil meter")
	}
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState != nil {
		m.Add(cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime())
	}
	return out, err
}
