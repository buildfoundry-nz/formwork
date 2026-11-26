//go:build ignore

package annotations

// ReinstateBySection reads the annotation type list straight off the request —
// the client chooses the blast radius again, and an empty list means
// "everything" (#1114 regression).
func (h *Handler) ReinstateBySection(ctx Ctx, req *ReinstateSectionAnnotationsRequest) (*Resp, error) {
	types := req.GetMarkerTypes()
	scope := annotationrestore.Scope{Types: types}
	return h.restore(ctx, req.GetSectionTag(), scope)
}
