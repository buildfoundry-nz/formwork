//go:build ignore

package quote

// Generate attaches the readiness verdict to the response BEFORE it computes it
// — shared.QuoteReadinessProto runs first and priceready.Check runs
// after, so the attached verdict is the zero value: the advisory signal is
// dropped (Phase 1 / sweep-4 #1).
func (h *Handler) Generate(ctx Ctx, req *Req) (*Resp, error) {
	resp := &Resp{}
	resp.Readiness = shared.QuoteReadinessProto(readiness) // want: quote-generate-readiness-computed-before-attach
	readiness := priceready.Check(ctx, req.ProjectID)
	quote, err := h.generator.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Quote = quote
	return resp, nil
}
