//go:build ignore

package gemini

// composeVisionRequest MINTS a media part but never routes it through the
// firstPromptParts seam — it hand-assembles a media-first []*genai.Part{...}
// literal. count(media-mint)=1 > count(firstPromptParts)=0, so the leading
// cache prefix is broken and the stable instructions are re-billed every call.
func composeVisionRequest(prompt string, img []byte) []*genai.Part { // want: gemini-media-parts-routed-through-prompt-first
	media := genai.NewPartFromBytes(img, "image/png")
	return []*genai.Part{media, genai.NewPartFromText(prompt)}
}
