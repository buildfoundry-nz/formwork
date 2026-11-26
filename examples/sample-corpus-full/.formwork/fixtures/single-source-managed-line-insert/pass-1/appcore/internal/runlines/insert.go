//go:build ignore

package runlines

import "github.com/sample/appcore/internal/curatedline"

// insertCrossbeamLine routes through the single-source writer — no inline INSERT,
// no 'auto_derived' line_origin token re-typed here.
func insertCrossbeamLine(ctx context.Context, tx pgx.Tx, bomID string) error {
	return curatedline.Insert(ctx, tx, bomID, sectionID, ordinal, description, unit, quantity, rate, curatedline.KindCrossbeam)
}
