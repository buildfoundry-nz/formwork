//go:build ignore

package annotations

// SetLabel mutates via a setter seam but hand-rolls the marshal tail instead
// of returning through shared.SuccessProtoResponse (the #3926 re-fork).
func SetLabel(ctx Ctx, tx Tx, req *Req) (int, string, []byte, error) { // want: annotation-setters-return-via-shared-response-helper
	if err := markupwrite.LogAndUpdateDisplayName(ctx, tx, req.ID, req.Name); err != nil {
		return 0, "", nil, err
	}
	body, err := shared.EncodeOpts.Marshal(&apiv1.SetLabelResponse{})
	if err != nil {
		return 0, "", nil, err
	}
	return http.StatusOK, "application/json", body, nil
}
