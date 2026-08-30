//go:build ignore

package pdftrace

// clusteredPageText is the ONE structured-text walk. Both the text path and the
// geometry path call this helper, so there is exactly one call site.
func clusteredPageText(i *Instance, ctx Context, page int) Text {
	// A commented-out earlier call must not inflate the count:
	// return i.GetPageTextStructured(ctx, page-1)
	return i.GetPageTextStructured(ctx, page)
}

func extractDocumentLayout(i *Instance, ctx Context, page int) Geometry {
	return shapeFrom(clusteredPageText(i, ctx, page))
}
