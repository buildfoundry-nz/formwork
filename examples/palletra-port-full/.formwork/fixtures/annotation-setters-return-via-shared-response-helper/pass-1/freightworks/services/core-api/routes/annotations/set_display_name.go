//go:build ignore

package annotations

// SetLabel mutates via a setter seam and returns its success response through
// the shared helper.
func SetLabel(ctx Ctx, tx Tx, req *Req) (int, string, []byte, error) {
	if err := markupwrite.LogAndUpdateDisplayName(ctx, tx, req.ID, req.Name); err != nil {
		return 0, "", nil, err
	}
	return shared.SuccessProtoResponse(&apiv1.SetLabelResponse{})
}
