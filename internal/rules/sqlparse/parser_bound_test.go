package sqlparse

import (
	"sync"
	"testing"
)

// #83 — go-pgquery serves concurrent callers from a pool of WASM instances at
// roughly 250 MB each. The pool grows to the peak concurrency it ever sees and
// never shrinks, and the cost is invisible to HeapAlloc because WASM linear
// memory is not live Go objects. Measured: 16 concurrent callers, 4,020 MB.
//
// It scales with CORE COUNT, not corpus size, and sql/* rules are CostFast — so
// unbounded they run at full worker width and a 16-core runner pays 4 GB to
// parse a handful of SELECTs.

func TestParserConcurrencyIsBounded(t *testing.T) {
	if got := ParseBound(); got != defaultParseConcurrency {
		t.Fatalf("parser bound = %d, want %d", got, defaultParseConcurrency)
	}
}

// The assertion that matters. A cap alone proves the channel was made with a
// size; this proves callers actually WAIT on it — drive far more goroutines than
// the bound and observe that concurrency inside the parser never exceeded it.
func TestParserConcurrencyNeverExceedsTheBound(t *testing.T) {
	ResetParsePeak()
	const callers = 32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := parse("SELECT id FROM t WHERE s = 'x';"); err != nil {
				t.Errorf("parse: %v", err)
			}
		}()
	}
	wg.Wait()

	peak := ParsePeak()
	if peak > int64(ParseBound()) {
		t.Fatalf("peak concurrency inside the parser was %d, bound is %d — the "+
			"semaphore is not admitting", peak, ParseBound())
	}
	// Guard against the opposite reading: if the work serialised for some other
	// reason the peak would be 1, and this test would pass while proving nothing
	// about the bound. 32 callers on any real machine should reach it.
	if peak < 2 {
		t.Skipf("peak was %d — the workload never contended, so this run proves "+
			"nothing about the bound", peak)
	}
}

func TestParserConcurrencyEnvOverride(t *testing.T) {
	t.Setenv(parseConcurrencyEnv, "9")
	if got := ParseConcurrencyFor(); got != 9 {
		t.Fatalf("env override = %d, want 9", got)
	}
	// A malformed value must not fail a run: this is a performance knob, and
	// refusing over a typo trades a memory problem for an availability one.
	t.Setenv(parseConcurrencyEnv, "not-a-number")
	if got := ParseConcurrencyFor(); got != defaultParseConcurrency {
		t.Fatalf("malformed override = %d, want the default %d", got, defaultParseConcurrency)
	}
	t.Setenv(parseConcurrencyEnv, "0")
	if got := ParseConcurrencyFor(); got != defaultParseConcurrency {
		t.Fatalf("zero override = %d, want the default %d", got, defaultParseConcurrency)
	}
}
