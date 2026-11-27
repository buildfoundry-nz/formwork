//go:build ignore

package stockroom

func decorate(ctx context.Context, t ParsedSheet) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.Int64("stockroom.vision.raw_bytes", int64(t.RawBytes)))
}
