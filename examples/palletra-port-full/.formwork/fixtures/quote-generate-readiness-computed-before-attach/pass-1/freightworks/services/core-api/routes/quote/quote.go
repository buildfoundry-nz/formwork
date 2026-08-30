//go:build ignore

package quote

// Generate computes the readiness verdict (priceready.Check) BEFORE it
// attaches it to the response (shared.QuoteReadinessProto), then generates the
// quote — the advisory draft/warnings signal is preserved and generation never
// hard-blocks.
func (h *Handler) Generate(ctx Ctx, req *Req) (*Resp, error) {
	readiness := priceready.Check(ctx, req.ProjectID)
	quote, err := h.generator.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	resp := &Resp{Quote: quote}
	resp.Readiness = shared.QuoteReadinessProto(readiness)
	return resp, nil
}
