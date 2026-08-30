//go:build ignore

package drawingquality

func Check() {
	findings = triageCauses(ctx, findings, nil)
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// findings = triageCauses(ctx, findings, slotVisiblePages(resolved))
