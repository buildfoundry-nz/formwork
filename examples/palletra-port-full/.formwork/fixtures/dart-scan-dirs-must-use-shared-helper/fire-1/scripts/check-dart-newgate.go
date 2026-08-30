//go:build ignore

package main

import (
	"fmt"
	"path/filepath"
)

// check-dart-newgate.go — a NEW gate that hand-rolls the Dart scanSourceDirs
// preamble instead of calling dartWalkDirs — this is the banned drift the
// ratchet catches.
func main() {
	scanSourceDirs := []string{"frontend/lib"}
	matches, err := filepath.Glob("packages/*/lib") // want: dart-scan-dirs-must-use-shared-helper
	if err != nil {
		fmt.Println(err)
		return
	}
	scanSourceDirs = append(scanSourceDirs, matches...)
	fmt.Println(scanSourceDirs)
}
