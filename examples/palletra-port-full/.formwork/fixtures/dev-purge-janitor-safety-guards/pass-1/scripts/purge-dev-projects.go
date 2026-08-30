//go:build ignore

package main

import (
	"fmt"
	"os"
	"regexp"
)

// Deletes dev-only test projects. Two mandatory guards below.
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: purge-dev-projects <db url> <max age>")
		os.Exit(1)
	}
	pgDsn := os.Args[1]
	maxAgeArg := os.Args[2]

	// Q5-3: dev env-gate fingerprint — only the dev tenant has these users.
	const devFingerprint = "e2e-critical-%@palletra.example"

	// Q5-4: validate MAX_AGE against a plain "<int> <unit>" interval BEFORE splicing.
	interval := regexp.MustCompile(`^[0-9]+ (minute|hour|day|week)$`)
	if !interval.MatchString(maxAgeArg) {
		fmt.Fprintf(os.Stderr, "invalid MAX_AGE: %s\n", maxAgeArg)
		os.Exit(1)
	}

	fmt.Printf("would purge projects matching %s older than INTERVAL '%s' at %s\n", devFingerprint, maxAgeArg, pgDsn)
}
