//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// check-example-gate — an illustrative gate that hardcodes a repo-relative target that
// still resolves against the tree and holds files.

// gate-target: freightworks/services/core-api/internal/gemini
const targetDir = "freightworks/services/core-api/internal/gemini"

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
