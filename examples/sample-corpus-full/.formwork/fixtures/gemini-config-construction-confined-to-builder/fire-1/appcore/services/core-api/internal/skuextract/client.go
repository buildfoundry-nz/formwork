//go:build ignore

package skuextract

// buildConfig assembles a model config field-by-field OUTSIDE the sanctioned
// gemini builders. The bare-type form has no brace-literal, yet Rule A keys on
// the type token, so this hand-rolled config that bypasses the output cap is
// still caught.
func buildConfig() {
	var cfg genai.GenerateContentConfig // want: gemini-config-construction-confined-to-builder
	cfg.Temperature = 0.2
	_ = cfg
}
