//go:build ignore

package parsecompose

import "context"

// persistLayout sets plain_text alongside layout_payload in the same INSERT,
// keeping the search DB-prefilter mirror in lockstep with the payload.
func persistLayout(ctx context.Context, tx execer, sheetID string, payload []byte, rawText string) error {
	_, err := tx.Exec(ctx, `INSERT INTO palletra.page_layout (page_id, layout_payload, plain_text) VALUES ($1, $2, $3)`)
	return err
}
