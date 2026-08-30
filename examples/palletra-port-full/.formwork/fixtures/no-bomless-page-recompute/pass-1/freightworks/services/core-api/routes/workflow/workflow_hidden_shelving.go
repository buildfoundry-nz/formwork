//go:build ignore

package workflow

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/palletra/freightworks/services/core-api/routes/shared"
)

// The sanctioned batch caller: recomputes every affected elevation page with the
// no-BOM variant, then re-prices the whole project BOM exactly once after the
// loop. except.paths carves this file out (#5179).
func generate(ctx context.Context, tx pgx.Tx, sheetIDs []string) error {
	for _, id := range sheetIDs {
		if err := shared.RecomputePageAggregatesWithoutBomEvalInTx(ctx, tx, id); err != nil {
			return err
		}
	}
	return shared.RepriceProjectBom(ctx, tx)
}
