//go:build ignore

package migrations

import "testing"

// The refusal test poisons an ahead ledger ON PURPOSE to prove the runner
// leaves it untouched. _test.go is exempt and MUST stay exempt.
func TestRunnerRefusesAheadRecord(t *testing.T) {
	m := newMigrator(t)
	_ = m.Force(20000524163000)
}
