//go:build ignore

package annotations

// UpdateElevation mutates one annotation's height but drops the marker_changed
// broadcast, so a second open window never refetches (the #6722 HIGH-2 defect).
func UpdateElevation(ctx Ctx, tx Tx, req *Req) error { // want: annotation-writers-must-broadcast-change-event
	if err := markupwrite.LogAndUpdateHeight(ctx, tx, req.ID, req.Height); err != nil {
		return err
	}
	return nil
}
