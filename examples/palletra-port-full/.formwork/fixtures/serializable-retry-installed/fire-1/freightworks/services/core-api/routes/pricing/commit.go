//go:build ignore

package pricing

import "context"

// commit opens a Serializable transaction but never wraps it in a retry
// helper, so a SQLSTATE 40001 serialization failure escapes as a bare 500.
func commit(ctx context.Context) error {
	opts := pgx.TxOptions{IsoLevel: pgx.Serializable} // want: serializable-retry-installed
	_ = opts
	return nil
}
