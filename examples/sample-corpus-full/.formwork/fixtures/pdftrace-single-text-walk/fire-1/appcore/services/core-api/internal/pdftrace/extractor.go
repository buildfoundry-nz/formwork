//go:build ignore

package pdftrace

// clusteredPageText is the intended single home of the structured-text walk.
func clusteredPageText(i *Instance, ctx Context, page int) Text {
	return i.GetPageTextStructured(ctx, page)
}
