//go:build ignore

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// check-dynamic-target-gate — its scan home is supplied by the caller, so there is
// no hardcoded repo-relative path to rot and nothing for a `// gate-target:`
// directive to declare. Correctly not checked: the meta-gate asks whether a
// HARDCODED target still resolves, and this gate has none.
//
// The mid-sentence mention of the directive above is deliberate and load-bearing:
// it is a PROSE reference, not a declaration, and an extractor that reads a
// directive's path to end of line without anchoring the directive to the start of
// its comment line reads it as one — declaring the backtick that follows. Do not
// reword it away.

func main() {
	root := os.Getenv("GATE_ROOT")
	if root == "" {
		os.Exit(0)
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "forbiddenToken") {
			os.Exit(1)
		}
		return nil
	})
}
