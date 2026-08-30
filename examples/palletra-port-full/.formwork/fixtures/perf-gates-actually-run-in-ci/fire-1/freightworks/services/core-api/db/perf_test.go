//go:build ignore

package db

import (
	"os"
	"testing"
)

// assertPerfGates self-skips the EXPLAIN query-plan gate unless RUN_PERF_GATES
// is set. Nothing in .github/workflows sets it, so this gate is DEAD in CI.
func assertPerfGates(t *testing.T) {
	if os.Getenv("RUN_PERF_GATES") == "" {
		t.Skip("perf gates disabled")
	}
}
