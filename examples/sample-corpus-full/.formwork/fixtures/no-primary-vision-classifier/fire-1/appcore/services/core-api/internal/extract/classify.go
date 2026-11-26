//go:build ignore

package extract

// classifyFrame is a PRIMARY-path function — its name encodes neither "fallback"
// nor "vision", yet it inline-calls the Gemini Vision classifier, re-promoting
// vision to the primary path and burning tokens on every PDF (Plan 3 regression).
func classifyFrame(ctx Ctx, pdf []byte) (Result, error) {
	pages, err := GeminiExtractPages(ctx, pdf) // want: no-primary-vision-classifier
	if err != nil {
		return Result{}, err
	}
	return classify(pages), nil
}
