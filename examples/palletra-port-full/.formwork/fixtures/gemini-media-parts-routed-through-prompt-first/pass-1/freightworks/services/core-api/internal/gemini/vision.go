//go:build ignore

package gemini

// composeVisionRequest mints its media part and routes it through the
// firstPromptParts seam, so count(media-mint)=1 <= count(firstPromptParts)=1.
func composeVisionRequest(instruction string, img []byte) []*genai.Part {
	part := genai.NewPartFromBytes(img, "image/png")
	return firstPromptParts(instruction, part)
}

// firstPromptParts is the single ordering seam: the text prompt always lands
// at index 0, and every attachment that follows keeps call-site order. It
// mints no media part itself (NewPartFromText is not a media mint), so
// count(mint)=0 <= count(seam)=0.
func firstPromptParts(instruction string, attachments ...*genai.Part) []*genai.Part {
	ordered := []*genai.Part{genai.NewPartFromText(instruction)}
	for _, a := range attachments {
		ordered = append(ordered, a)
	}
	return ordered
}
