//go:build ignore

package geometrycompact

import "context"

// packOne writes a compacted layout_payload back with a bare UPDATE and no
// compare-and-swap predicate, so a re-extract that committed between the read
// and this write is lost-update clobbered (sweep-2 #1).
func packOne(ctx context.Context, tx execer, sheetID string) error {
	_, err := tx.Exec(ctx, `UPDATE palletra.page_layout SET layout_payload = $1 WHERE page_id = $2`) // want: page-geometry-payload-unguarded-write
	return err
}
