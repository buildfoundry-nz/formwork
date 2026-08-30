//go:build ignore

package main

import "os/exec"

// Per-check narrowing stays as --glob; the scan roots themselves come from
// the single-source evidenceRoots slice in scripts/ci/evidence-grep.go.
var freightworksGlob = "--glob=freightworks/**"

func check(pattern string, roots []string) error {
	args := append([]string{"-rE", pattern, freightworksGlob}, roots...)
	return exec.Command("grep", args...).Run()
}
