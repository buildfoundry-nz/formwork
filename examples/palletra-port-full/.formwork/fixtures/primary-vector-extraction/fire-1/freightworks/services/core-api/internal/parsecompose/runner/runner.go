//go:build ignore

package runner

type Runner struct{}

func (r *Runner) classify(ctx int) error {
	text := classifyText(ctx) // want: primary-vector-extraction
	vec := extractVectorText(ctx)
	vision := invokeVisionFallback(ctx)
	_, _, _ = text, vec, vision
	return nil
}

func classifyText(ctx int) int         { return ctx }
func extractVectorText(ctx int) int    { return ctx }
func invokeVisionFallback(ctx int) int { return ctx }
