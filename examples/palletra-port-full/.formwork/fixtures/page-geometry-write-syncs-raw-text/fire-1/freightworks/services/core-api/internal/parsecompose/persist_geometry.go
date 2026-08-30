//go:build ignore

package parsecompose

import "context"

// persistLayout STOREs a page_layout row but never sets plain_text, so the
// search DB-prefilter mirror goes stale and the page is silently dropped from
// search results (audit-#17).
func persistLayout(ctx context.Context, tx execer, sheetID string, payload []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO palletra.page_layout (page_id, layout_payload) VALUES ($1, $2)`) // want: page-geometry-write-syncs-raw-text
	return err
}
