//go:build ignore

package rasterizer

// Render resolves the base DPI, clamps it via the BoundedRenderDPI memory-safety
// clamp (r.resolveBoundedDPI), THEN shells out to gm (r.runCLI) — runCLI only ever
// consumes the clamped DPI (Plan 4 Phase 11 / PR #7922).
func (r *Rasterizer) Render(ctx Ctx, in *Input) (*Output, error) {
	baseResolution, err := r.deriveDPI(ctx, in)
	if err != nil {
		return nil, err
	}
	dpi, _, err := r.resolveBoundedDPI(ctx, in, baseResolution)
	if err != nil {
		return nil, err
	}
	pngBytes, err := r.runCLI(ctx, in.bin, in.path, dpi)
	if err != nil {
		return nil, err
	}
	return &Output{PNG: pngBytes}, nil
}
