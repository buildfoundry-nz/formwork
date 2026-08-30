//go:build ignore

package annotations

// UpdateElevation mutates one annotation's height and broadcasts marker_changed
// in the same handler so SSE clients refetch.
func UpdateElevation(ctx Ctx, tx Tx, req *Req) error {
	if err := markupwrite.LogAndUpdateHeight(ctx, tx, req.ID, req.Height); err != nil {
		return err
	}
	return publishAnnotationChangedEvent(ctx, tx, req.ID)
}
