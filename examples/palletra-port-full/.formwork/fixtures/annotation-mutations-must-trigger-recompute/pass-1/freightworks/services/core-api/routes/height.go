//go:build ignore

package routes

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// UpdateElevation flips an annotation's height and recomputes the page in the same
// tx, so page_gauges is never left stale.
func UpdateElevation(ctx context.Context, tx pgx.Tx, id string, sheetID string, h float64) error {
	if _, err := tx.Exec(ctx,
		`UPDATE palletra.annotations SET height = $2 WHERE id = $1`, id, h); err != nil {
		return err
	}
	return recomputePageRollupsInTx(ctx, tx, sheetID)
}
