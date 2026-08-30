//go:build ignore

package supersede

import "context"

// FIRE: the swap writer commits a new version without validating the successor.
func Supersede(ctx context.Context, tx *Tx, spec *LayoutSpec) error {
	return tx.Commit()
}
