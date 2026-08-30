//go:build ignore

package extract

// invokeTextClassifier is the PRIMARY path — vector-first, no vision call.
func invokeTextClassifier(ctx Ctx, pdf []byte) (Result, error) {
	return classify(scanVectorText(pdf)), nil
}

// invokeVisionFallback is the gated helper: its name encodes the fallback role, so
// it is the ONLY place GeminiExtractPages may be invoked. The caller gates
// it (if pagesWithContent == 0 / if len(unresolvedTypes) > 0).
func invokeVisionFallback(ctx Ctx, pdf []byte) (Result, error) {
	pages, err := GeminiExtractPages(ctx, pdf)
	if err != nil {
		return Result{}, err
	}
	return classify(pages), nil
}
