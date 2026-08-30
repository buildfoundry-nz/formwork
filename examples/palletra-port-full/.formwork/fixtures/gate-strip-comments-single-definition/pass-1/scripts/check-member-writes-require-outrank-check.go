//go:build ignore

// check-member-writes-require-outrank-check.go — uses the shared lib and only CALLS
// dropComments (a call site is not a definition and does not count).
package main

import "os"

func main() {
	_, _ = dropComments(os.Args[1])
}
