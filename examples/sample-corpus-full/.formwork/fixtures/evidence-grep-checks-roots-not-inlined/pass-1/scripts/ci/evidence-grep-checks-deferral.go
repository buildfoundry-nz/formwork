//go:build ignore

package main

import "os/exec"

// Per-check narrowing stays as --glob; the scan roots themselves come from
// the single-source evidenceRoots slice in scripts/ci/evidence-grep.go.
var appcoreGlob = "--glob=appcore/**"

func check(pattern string, roots []string) error {
	args := append([]string{"-rE", pattern, appcoreGlob}, roots...)
	return exec.Command("grep", args...).Run()
}
