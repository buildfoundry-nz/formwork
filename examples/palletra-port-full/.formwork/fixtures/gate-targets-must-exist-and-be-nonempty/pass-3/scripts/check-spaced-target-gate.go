//go:build ignore

package main

import (
	"os"
	"strings"
)

// check-spaced-target-gate — the same gate as fire-3 with its target still in
// place. The declared path carries a space and must be read to end-of-line, not
// truncated at the first blank, or a live target reads as absent.

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
