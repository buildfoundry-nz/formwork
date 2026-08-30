//go:build ignore

package stockroom

func build(size int) ParsedSheet {
	return ParsedSheet{RawBytes: size}
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// trace.SpanFromContext(ctx).SetAttributes(attribute.Int64("stockroom.vision.raw_bytes", int64(t.RawBytes)))
