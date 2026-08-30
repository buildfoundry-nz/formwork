//go:build ignore

package rasterizer

// The per-render OTel span was removed — Cloud Trace has no render anchor.
func (r *Rasteriser) Render(ctx context.Context) error {
	return r.render(ctx)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// ctx, span := otel.Tracer("pageraster").Start(ctx, "PagePaint")
