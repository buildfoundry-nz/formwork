//go:build ignore

package routes

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// UpdateElevation flips an annotation's height but never recomputes — the metric
// rows on the page are left stale. This is the violation the gate catches.
func UpdateElevation(ctx context.Context, tx pgx.Tx, id string, h float64) error {
	_, err := tx.Exec(ctx,
		`UPDATE palletra.annotations SET height = $2 WHERE id = $1`, id, h)
	return err
}
