//go:build ignore

package main

import "os/exec"

// check re-open-codes the packages/ root as a scan-root element instead of
// consuming the single-source evidenceRoots slice.
func check(pattern string) error {
	args := []string{
		"-rE",
		pattern,
		"packages/", // want: evidence-grep-checks-roots-not-inlined
	}
	return exec.Command("grep", args...).Run()
}
