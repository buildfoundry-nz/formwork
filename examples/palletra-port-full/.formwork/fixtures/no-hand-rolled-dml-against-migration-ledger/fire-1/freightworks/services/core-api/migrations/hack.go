//go:build ignore

package migrations

// The ledger rewrite, hand-rolled as DML inside a string literal.
const unwindLedger = "DELETE FROM app_schema_migrations WHERE version = $1" // want: no-hand-rolled-dml-against-migration-ledger
