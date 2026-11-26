//go:build ignore

package pdftrace

// extractDocumentLayout re-forks the PDFium structured-text walk instead of
// calling clusteredPageText — a second call site that WILL drift from the text path.
func extractDocumentLayout(i *Instance, ctx Context, page int) Geometry {
	t := i.GetPageTextStructured(ctx, page)
	return shapeFrom(t)
}
