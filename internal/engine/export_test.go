package engine

import (
	"time"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// export_test.go — test-only handles on the engine's resource governors: the
// machine-wide heavy gate and the phase-1 cost partition. Neither is part of
// engine's exported surface: they decide how a run spends memory and cores,
// which is not a contract a caller configures.

// HeavyFinalizerWorkers is the width the machine-wide gate reuses as its slot
// count, exported so a test asserts against the same constant the engine does
// rather than a copy of the number.
const HeavyFinalizerWorkers = heavyFinalizerWorkers

// SetHeavyGateDir repoints the slot directory and returns a restore func, the
// same shape as the two below. The restore is not decoration: a test that
// repoints the gate and leaves it repointed disarms the machine-wide bound for
// every test after it, and no assertion downstream can see that it did.
// TestMain is the one caller that deliberately drops the restore, because its
// repointing is meant to outlive every test in the binary.
func SetHeavyGateDir(dir string) func() {
	prev := heavyGateDir
	heavyGateDir = dir
	return func() { heavyGateDir = prev }
}

// SetHeavyGateWarn swaps the disclosure sink and returns a restore func.
func SetHeavyGateWarn(f func(string, ...any)) func() {
	prev := heavyGateWarn
	heavyGateWarn = f
	return func() { heavyGateWarn = prev }
}

// SetHeavyGateTiming shortens the fail-open deadline so a test does not have to
// wait the operator's default out. Returns a restore func.
func SetHeavyGateTiming(wait, poll time.Duration) func() {
	prevWait, prevPoll := heavyGateWait, heavyGatePoll
	heavyGateWait, heavyGatePoll = wait, poll
	return func() { heavyGateWait, heavyGatePoll = prevWait, prevPoll }
}

// TestGate drives the lock primitive directly, without an engine.Run around
// it: #81's acceptance criterion asks for the bound to be exercised across
// acquirers that share no in-process semaphore, and two TestGates over one
// directory share nothing but the files.
type TestGate struct{ g *heavyGate }

func NewTestGate(dir string, slots int, wait, poll time.Duration, warn func(string, ...any)) *TestGate {
	return &TestGate{g: &heavyGate{dir: dir, slots: slots, wait: wait, poll: poll, warn: warn}}
}

func (t *TestGate) Acquire() func() { return t.g.acquire() }

// SelfBoundedOf reports whether a checker takes its OWN concurrency admission
// inside CheckFile — the discriminator engine.Run's phase-1 partition reads to
// choose which of the two pools dispatches a rule.
//
// Exported so phase1_sqlparse_wiring_test.go can assert that the ENGINE
// classifies the REAL registered checkers, end to end, rather than that some
// method exists on a type. Mirrors HeavyFinalizerWorkers above: a test asserts
// against the thing the engine reads, not a copy of it.
func SelfBoundedOf(c rules.Checker) bool { return selfBoundedOf(c) }
