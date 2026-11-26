//go:build ignore

package main

func resolveLocalDeps(localDependencies LocalDependencies) DispatcherDeps {
	return DispatcherDeps{}
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// PageFeedCache: localDependencies.PageFeedCache,
