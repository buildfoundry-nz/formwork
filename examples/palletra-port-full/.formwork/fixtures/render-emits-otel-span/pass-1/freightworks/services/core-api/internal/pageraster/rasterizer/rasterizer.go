//go:build ignore

package rasterizer

// Opens the per-render OTel "PagePaint" span with a deferred End.
func (r *Rasteriser) Render(ctx context.Context) error {
	ctx, span := otel.Tracer("pageraster").Start(ctx, "PagePaint")
	defer span.End()
	return r.render(ctx)
}
