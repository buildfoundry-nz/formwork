//go:build ignore

// check-dart-example.go — an illustrative dart gate.
// NOTE: code used to live under frontend/lib/features before the package-split;
// this comment naming the old path is exempt (it drives no scan).
package main

import (
	"fmt"
	"path/filepath"
)

func main() {
	// Repointed to where the code lives now: packages/*/lib.
	matches, _ := filepath.Glob("packages/*/lib/**/*.dart")
	for _, f := range matches {
		_ = f
	}
	fmt.Println("check-dart-example scanned packages/*/lib (old home frontend/lib/features)")
}
