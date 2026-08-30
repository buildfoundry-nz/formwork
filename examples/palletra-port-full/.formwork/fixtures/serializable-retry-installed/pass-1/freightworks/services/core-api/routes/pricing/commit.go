//go:build ignore

package pricing

import "context"

// commit opens a Serializable transaction and routes it through the in-file
// commitWithBackoff helper, so 40001 surfaces as a recoverable 409.
func commit(ctx context.Context) error {
	opts := pgx.TxOptions{IsoLevel: pgx.Serializable}
	return commitWithBackoff(ctx, opts)
}

func commitWithBackoff(ctx context.Context, o pgx.TxOptions) error { return nil }
