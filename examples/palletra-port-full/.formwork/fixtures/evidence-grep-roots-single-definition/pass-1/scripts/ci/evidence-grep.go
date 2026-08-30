//go:build ignore

package main

import "os/exec"

var evidenceRoots = []string{
	"freightworks/",
	"packages/",
	"shared/",
}

func check(pattern string) error {
	args := append([]string{"-rE", pattern}, evidenceRoots...)
	return exec.Command("grep", args...).Run()
}
