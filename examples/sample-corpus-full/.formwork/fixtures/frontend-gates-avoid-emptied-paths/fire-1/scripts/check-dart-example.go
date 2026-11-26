//go:build ignore

// check-dart-example.go — an illustrative dart gate.
package main

import "path/filepath"

func main() {
	// VIOLATION: this real scan line names a package-split-EMPTIED code home. After
	// the split, feature code moved to packages/*/lib, so this scans an empty dir and
	// the gate passes vacuously.
	matches, _ := filepath.Glob("frontend/lib/features/**/*.dart")
	for _, f := range matches {
		_ = f
	}
}
