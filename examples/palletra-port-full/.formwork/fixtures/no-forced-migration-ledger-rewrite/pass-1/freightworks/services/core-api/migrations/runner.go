//go:build ignore

package migrations

// LEDGER INVARIANT: the runner must never call .Force( on the ledger; it
// records intent, not schema state. This prose names the API on purpose and
// must not trip the gate (decomment-go strips it before matching).
func run(m *migrate.Migrate) error {
	return m.Up()
}
