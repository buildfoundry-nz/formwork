//go:build ignore

package main

func wire(extractionDispatcher Enqueuer) CoreRouteDeps {
	return CoreRouteDeps{
		PageFeedCache: extractionDispatcher.PageFeedCache,
	}
}
