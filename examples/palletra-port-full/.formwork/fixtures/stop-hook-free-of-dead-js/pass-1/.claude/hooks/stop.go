//go:build ignore

package main

import (
	"os"
	"os/exec"
)

var archScripts []string

// stop.go — Claude Code stop hook: run the architecture gates, then the live
// Flutter gate. The JS toolchain was removed in the zero-JS migration;
// nothing here references it.
func main() {
	for _, script := range archScripts {
		if err := exec.Command("go", "run", "scripts/"+script).Run(); err != nil {
			os.Exit(1)
		}
	}

	if err := exec.Command("flutter", "analyze").Run(); err != nil {
		os.Exit(1)
	}
}
