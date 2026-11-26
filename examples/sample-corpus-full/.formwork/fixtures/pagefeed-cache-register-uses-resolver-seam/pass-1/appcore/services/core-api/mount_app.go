//go:build ignore

package main

func mountApp(d CoreRouteDeps) {
	wave11PageFeedCache := resolvePageFeedCache(d.PageFeedCache, d.DrawingsSigner)
	_ = wave11PageFeedCache
}
