//go:build ignore

package main

func mountApp(d CoreRouteDeps) {
	wave11PageFeedCache := files.NewPageFeedCache(d.DrawingsSigner) // want: pagefeed-cache-register-forbids-direct-construction
	_ = wave11PageFeedCache
}
