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
