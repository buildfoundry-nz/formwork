//go:build ignore

package main

import (
	"os"
	"strings"
)

// check-spaced-target-gate — VIOLATION: its declared target no longer exists. The
// path carries a SPACE, and a directive charset that stops at whitespace truncates
// it to the live prefix `freightworks/shared/design`, so the meta-gate resolves a
// path this gate never reads and reports green over a rotted target.

// gate-target: freightworks/shared/design notes.md
const targetFile = "freightworks/shared/design notes.md"

func main() {
	data, err := os.ReadFile(targetFile)
	if err != nil {
		os.Exit(0)
	}
	if strings.Contains(string(data), "forbiddenToken") {
		os.Exit(1)
	}
}
