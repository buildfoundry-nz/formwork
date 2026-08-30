//go:build ignore

package main

func mountApp(d CoreRouteDeps) {
	wave11PageFeedCache := d.PageFeedCache
	_ = wave11PageFeedCache
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// wave11PageFeedCache := resolvePageFeedCache(d.PageFeedCache, d.DrawingsSigner)
