//go:build ignore

package rasterizer

// Render shells out to gm (r.runCLI) BEFORE it clamps the DPI
// (r.resolveBoundedDPI) — so runCLI consumes the unclamped base DPI, and an
// oversized A0 plan OOMs the worker (Plan 4 Phase 11 / PR #7922).
func (r *Rasterizer) Render(ctx Ctx, in *Input) (*Output, error) {
	baseResolution, err := r.deriveDPI(ctx, in)
	if err != nil {
		return nil, err
	}
	pngBytes, err := r.runCLI(ctx, in.bin, in.path, baseResolution) // want: render-dpi-clamp-runs-before-shellout
	if err != nil {
		return nil, err
	}
	dpi, _, err := r.resolveBoundedDPI(ctx, in, baseResolution)
	_ = dpi
	return &Output{PNG: pngBytes}, nil
}
