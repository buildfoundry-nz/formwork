//go:build ignore

package annotations

// ReinstateBySection names the step (segment_code) and resolves which annotation
// types that step OWNS from the canonical approve registry — the server owns
// the scope, and the client cannot name what a restore deletes (#1114).
func (h *Handler) ReinstateBySection(ctx Ctx, req *ReinstateSectionAnnotationsRequest) (*Resp, error) {
	cfg := approveall.GetConfig(req.GetSectionTag())
	scope := annotationrestore.Scope{
		Types:  cfg.ScopedAnnotationTypeCodes(),
		Family: cfg.GroupFilter,
	}
	return h.restore(ctx, req.GetSectionTag(), scope)
}
