//go:build ignore

package main

func mountApp(r Router, d CoreRouteDeps, wave11PageFeedCache *Cache) {
	vectorregions.MountVectorRegions(r, d, wave11PageFeedCache)
}
