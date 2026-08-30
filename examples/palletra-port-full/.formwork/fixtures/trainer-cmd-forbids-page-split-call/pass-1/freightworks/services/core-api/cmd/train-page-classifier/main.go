//go:build ignore

package main

import "github.com/palletra/freightworks/internal/parsecompose/classifier/local/train"

func main() {
	set := gatherPages()
	fitSet, holdoutSet, degraded := train.DivideWithFallback(set, 0.2)
	if degraded {
		warn("holdout NOT held out cleanly; falling back")
	}
	fit(fitSet, holdoutSet)
}
