//go:build ignore

package supersede

import "context"

// FIRE: the swap writer commits a new version without validating the successor.
func Supersede(ctx context.Context, tx *Tx, spec *LayoutSpec) error {
	return tx.Commit()
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// if err := validateTemplateNoNativeKitMix(ctx, tx, spec); err != nil {
