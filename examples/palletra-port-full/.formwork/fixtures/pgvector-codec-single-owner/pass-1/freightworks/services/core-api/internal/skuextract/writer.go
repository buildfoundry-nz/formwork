//go:build ignore

package skuextract

import (
	"context"

	"github.com/palletra/freightworks/services/core-api/internal/pgvector"
)

// writeVector passes an already-built pgvector literal to the ::vector cast.
// It never touches strconv — the literal was built by pgvector.Format — so this
// file carries only signal 2, not signal 1, and is not a codec.
func writeVector(ctx context.Context, tx Tx, id int64, vec []float32) error {
	lit := pgvector.Format(vec)
	_, err := tx.Exec(ctx, `UPDATE extracted_skus SET embedding = $2::vector WHERE id = $1`, id, lit)
	return err
}
