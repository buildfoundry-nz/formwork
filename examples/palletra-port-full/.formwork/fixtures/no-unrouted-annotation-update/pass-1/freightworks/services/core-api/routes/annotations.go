//go:build ignore

package routes

import "context"

func relabelAnnotation(ctx context.Context, tx dbTx, id, label string) error {
	// Routes through the seam so annotation_timeline is written in the same tx.
	return markupwrite.LogAndUpdateLabel(ctx, tx, id, label)
}
