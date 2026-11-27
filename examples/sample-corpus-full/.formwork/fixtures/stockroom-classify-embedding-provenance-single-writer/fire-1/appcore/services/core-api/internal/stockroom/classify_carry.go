//go:build ignore

package stockroom

import "context"

// commitRollover forks the 'embedding' provenance literal into the carry stage —
// a second writer of source='embedding' outside stockroom_classify_subcategories.go.
func commitRollover(ctx context.Context, tx Tx, id int64) error {
	_, err := tx.Exec(ctx, "UPDATE priced_lines SET source = 'embedding' WHERE id = $1", id) // want: stockroom-classify-embedding-provenance-single-writer
	return err
}
