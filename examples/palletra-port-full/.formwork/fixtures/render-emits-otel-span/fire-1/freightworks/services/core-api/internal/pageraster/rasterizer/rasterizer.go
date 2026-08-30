//go:build ignore

package rasterizer

// The per-render OTel span was removed — Cloud Trace has no render anchor.
func (r *Rasteriser) Render(ctx context.Context) error {
	return r.render(ctx)
}
