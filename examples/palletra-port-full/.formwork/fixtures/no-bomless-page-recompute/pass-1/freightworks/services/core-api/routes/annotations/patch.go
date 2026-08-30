//go:build ignore

package annotations

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/palletra/freightworks/services/core-api/routes/shared"
)

// A single-page mutation route re-prices the BOM in the same call. It must NOT
// use shared.RecomputePageAggregatesWithoutBomEvalInTx (that name in this prose
// comment is stripped by decomment-go, so it is not a violation).
func recompute(ctx context.Context, tx pgx.Tx, sheetID string) error {
	return shared.RecomputePageRollupsInTx(ctx, tx, sheetID)
}
