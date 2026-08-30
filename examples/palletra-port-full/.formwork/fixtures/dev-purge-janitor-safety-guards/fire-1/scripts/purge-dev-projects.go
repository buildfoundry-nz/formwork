//go:build ignore

package main

import (
	"fmt"
	"os"
)

// Deletes dev-only test projects.
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: purge-dev-projects <db url> <max age>")
		os.Exit(1)
	}
	pgDsn := os.Args[1]
	maxAgeArg := os.Args[2]

	const devFingerprint = "e2e-critical-%@palletra.example"

	// REGRESSION: MAX_AGE spliced straight into the SQL with no interval validation.
	fmt.Printf("would purge projects matching %s older than INTERVAL '%s' at %s\n", devFingerprint, maxAgeArg, pgDsn)
}
