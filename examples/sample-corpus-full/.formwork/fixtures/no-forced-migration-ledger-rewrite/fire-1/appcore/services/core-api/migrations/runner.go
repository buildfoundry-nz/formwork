//go:build ignore

package migrations

// A stale build force-rewinds the shared ledger — the #6608 outage shape.
func rewind(m *migrate.Migrate) error {
	return m.Force(20000524163000) // want: no-forced-migration-ledger-rewrite
}
