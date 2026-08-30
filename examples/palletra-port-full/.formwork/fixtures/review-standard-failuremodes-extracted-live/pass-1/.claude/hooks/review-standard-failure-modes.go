//go:build ignore

// Failure-modes SessionStart hook.
// Extracts its slice LIVE from the canonical skill file so it can never drift.
package main

import (
	"fmt"
	"os"
	"strings"
)

const skillPath = ".claude/skills/layered-review/SKILL.md"

func main() {
	data, err := os.ReadFile(skillPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printing := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "## What this review checks" {
			printing = true
		}
		if printing {
			fmt.Println(line)
		}
	}
}
