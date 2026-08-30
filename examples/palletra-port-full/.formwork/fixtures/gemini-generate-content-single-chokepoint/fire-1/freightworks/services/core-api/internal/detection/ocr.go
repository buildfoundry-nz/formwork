//go:build ignore

package detection

// classifyScan is a PLAIN detection seam — its name is not the chokepoint, yet it
// calls the Vertex SDK directly (here via the aliased Models handle), bypassing
// the generateContent chokepoint and its non-STOP fail-loud classification. A
// MAX_TOKENS / RECITATION / SAFETY finish would return truncated text as success.
func classifyScan(ctx Ctx, req Request) (Result, error) {
	m := &c.sdk.Models
	resp, err := m.GenerateContent(ctx, req) // want: gemini-generate-content-single-chokepoint
	if err != nil {
		return Result{}, err
	}
	return parse(resp.Text()), nil
}
