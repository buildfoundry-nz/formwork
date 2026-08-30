//go:build ignore

package main

import (
	"os/exec"
	"strings"
)

// roots is an open-coded string, not the single-source evidenceRoots slice.
var roots = "freightworks shared"

func check(pattern string) error {
	args := append([]string{"-rE", pattern}, strings.Fields(roots)...)
	return exec.Command("grep", args...).Run()
}
