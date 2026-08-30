//go:build ignore

package main

import "github.com/palletra/freightworks/internal/shared"

// A non-core-api binary that imports internal/shared — this is what confers
// genuine sharing, so internal/shared is correctly placed.
func main() {
	_ = shared.Value()
}
