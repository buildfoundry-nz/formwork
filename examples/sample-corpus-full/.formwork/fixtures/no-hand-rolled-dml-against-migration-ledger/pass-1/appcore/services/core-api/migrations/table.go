//go:build ignore

package migrations

// rowSet passes the ledger table name as a plain arg, never inside a DML verb.
// The bare table name in a string literal is fine — a DML verb must appear in
// the same literal to be a violation.
const schemaLedgerTable = "app_schema_migrations"

func run() { _ = schemaLedgerTable }
