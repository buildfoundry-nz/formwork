//go:build ignore

package main

import "github.com/palletra/freightworks/internal/parsecompose/classifier/local/train"

func main() {
	set := gatherPages()
	fitSet, holdoutSet := train.Split(set, 0.2) // want: trainer-cmd-forbids-page-split-call
	fit(fitSet, holdoutSet)
}
