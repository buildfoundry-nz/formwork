//go:build ignore

package main

func mountApp(r Router, d CoreRouteDeps, wave11PageFeedCache *Cache) {
	files.RegisterFiles(r, d)
}
