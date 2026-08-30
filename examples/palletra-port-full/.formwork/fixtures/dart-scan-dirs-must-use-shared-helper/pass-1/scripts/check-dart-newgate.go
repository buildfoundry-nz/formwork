//go:build ignore

package main

import "fmt"

// check-dart-newgate.go — a NEW gate that consumes the single-source
// dartWalkDirs enumeration — clean. The canonical preamble builds
// packages/*/lib via dartWalkDirs, not a hand-rolled glob.
func main() {
	scanSourceDirs := []string{"frontend/lib"}
	scanSourceDirs = append(scanSourceDirs, dartWalkDirs(".")...)
	fmt.Println(scanSourceDirs)
}
