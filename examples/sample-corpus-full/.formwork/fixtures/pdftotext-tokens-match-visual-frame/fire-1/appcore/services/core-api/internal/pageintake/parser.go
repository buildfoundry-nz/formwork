//go:build ignore

package pageintake

// parseSheet reads the pdftotext -bbox page box and normalizes the word tokens
// against those RAW dims, never projecting through the rotated visual frame. On
// a /Rotate 90|270 page the raw box has its axes transposed, so every token
// mis-projects and the whole page drops to OCR (#4582).
func parseSheet(out []byte) Page {
	m := boundsPageRe.FindSubmatch(out) // want: pdftotext-tokens-match-visual-frame
	w, h := m[1], m[2]
	return canonicalizeTokens(out, w, h)
}
