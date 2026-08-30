//go:build ignore

package pageintake

// parseSheet reads the pdftotext -bbox page box and projects the word tokens
// through displayPageDims, which picks the orientation matching the render dims,
// so tokens land in the same frame as the raster + detection boxes on any
// rotation (#4582).
func parseSheet(out []byte, render RenderSize) Page {
	m := boundsPageRe.FindSubmatch(out)
	w, h := displayPageDims(m[1], m[2], render)
	return canonicalizeTokens(out, w, h)
}
