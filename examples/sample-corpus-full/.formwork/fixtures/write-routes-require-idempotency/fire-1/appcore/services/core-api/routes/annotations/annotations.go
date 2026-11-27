//go:build ignore

package annotations

func (h *Handler) CreateAnnotation(w http.ResponseWriter, r *http.Request) {
	req := &apiv1.CreateAnnotationRequest{}
	decode(r, req)
	if err := h.create(ctx, tx, req); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (h *Handler) GetAnnotation(w http.ResponseWriter, r *http.Request) {
	req := &apiv1.GetAnnotationRequest{}
	_ = req
}
