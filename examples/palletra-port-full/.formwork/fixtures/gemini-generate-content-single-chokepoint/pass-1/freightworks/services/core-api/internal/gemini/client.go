//go:build ignore

package gemini

// generateContent is the single Vertex chokepoint: the ONLY func allowed to call
// Models.GenerateContent. It classifies the finish via cutoffErr so a
// non-STOP reply cannot silently return truncated text as success.
func (c *providerClient) generateContent(ctx Ctx, req Request) (Response, error) {
	resp, err := c.sdk.Models.GenerateContent(ctx, req)
	if err != nil {
		return resp, err
	}
	terr := cutoffErr(resp)
	return resp, terr
}

// produceVision is a plain seam that routes through the chokepoint — it never
// touches Models.GenerateContent directly, so it does not bypass fail-loud.
func (c *providerClient) produceVision(ctx Ctx, req Request) (Response, error) {
	return c.generateContent(ctx, req)
}
