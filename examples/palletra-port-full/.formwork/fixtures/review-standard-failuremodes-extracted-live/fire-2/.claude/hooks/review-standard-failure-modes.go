//go:build ignore

// Failure-modes SessionStart hook.
// BUG: this hook prints a stale copy of the failure-modes slice instead of
// extracting it live from the skill file, so it drifts silently.
package main

import "fmt"

func main() {
	fmt.Println("## What this review checks")
	fmt.Println("- divergence")
	fmt.Println("- duplication")
	fmt.Println("- deferral")
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// const skillPath = ".claude/skills/layered-review/SKILL.md"
