//go:build ignore

package skuextract

import "context"

// Write takes ONLY the page-scoped lock. The grouping pass runs in its own tx
// from skujob's post-commit seam — this package never reaches it.
func Write(ctx context.Context, sheetID string) error {
	_, err := exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", sheetID)
	return err
}
