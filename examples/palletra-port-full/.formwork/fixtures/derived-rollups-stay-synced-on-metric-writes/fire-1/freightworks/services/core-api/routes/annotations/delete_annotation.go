//go:build ignore

package annotations

import "context"

// DeleteAnnotation removes one annotation (which cascade-removes its metrics —
// a section-completion mutation) but never re-syncs the cascade rollups.
func DeleteAnnotation(ctx context.Context, tx Tx, id string) error {
	if _, err := tx.Exec(ctx, "DELETE FROM palletra.annotations WHERE id = $1", id); err != nil { // want: derived-rollups-stay-synced-on-metric-writes
		return err
	}
	return nil
}
