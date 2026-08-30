//go:build ignore

package annotations

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/palletra/freightworks/services/core-api/routes/shared"
)

func recompute(ctx context.Context, tx pgx.Tx, sheetID string) error {
	return shared.RecomputePageAggregatesWithoutBomEvalInTx(ctx, tx, sheetID) // want: no-bomless-page-recompute
}
