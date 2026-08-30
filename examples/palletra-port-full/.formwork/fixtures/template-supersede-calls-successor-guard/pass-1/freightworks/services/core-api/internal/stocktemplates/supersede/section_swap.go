//go:build ignore

package supersede

import "context"

// Supersede writes a new template version, validating the successor before commit.
func Supersede(ctx context.Context, tx *Tx, spec *LayoutSpec) error {
	if err := validateTemplateNoNativeKitMix(ctx, tx, spec); err != nil {
		return err
	}
	return tx.Commit()
}
