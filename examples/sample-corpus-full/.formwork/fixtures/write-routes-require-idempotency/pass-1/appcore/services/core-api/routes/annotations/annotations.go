//go:build ignore

package annotations

func (h *Handler) CreateAnnotation(w http.ResponseWriter, r *http.Request) {
	req := &apiv1.CreateAnnotationRequest{}
	decode(r, req)
	err := idempotency.Execute(ctx, tx, idempotency.Params{
		RequestPayload: idempotency.MustCanonicalMarshal(req),
	}, func() error {
		return h.create(ctx, tx, req)
	})
	_ = err
}

func (h *Handler) GetAnnotation(w http.ResponseWriter, r *http.Request) {
	req := &apiv1.GetAnnotationRequest{}
	_ = req
}
