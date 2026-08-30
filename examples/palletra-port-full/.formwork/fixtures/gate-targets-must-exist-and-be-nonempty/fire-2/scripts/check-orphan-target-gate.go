//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// check-orphan-target-gate — VIOLATION: it hardcodes a repo-relative target and
// declares no `// gate-target:` directive at all, so the meta-gate reads nothing
// out of it and never asks whether that target still resolves. The target was
// relocated; this gate has taken its "target absent -> skip -> exit 0" branch on
// every run since, which is the exact rot the meta-gate exists to catch.

const targetDir = "freightworks/services/core-api/internal/gone"

func main() {
	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		os.Exit(0)
	}
	found := false
	_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "forbiddenToken") {
			found = true
		}
		return nil
	})
	if found {
		fmt.Fprintln(os.Stderr, "forbiddenToken found")
		os.Exit(1)
	}
}
