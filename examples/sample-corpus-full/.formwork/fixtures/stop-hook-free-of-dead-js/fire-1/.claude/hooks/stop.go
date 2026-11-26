//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
)

var archScripts []string

// stop.go — Claude Code stop hook: run the architecture gates.
func main() {
	for _, script := range archScripts {
		if err := exec.Command("go", "run", "scripts/"+script).Run(); err != nil {
			os.Exit(1)
		}
	}

	out, err := exec.Command("npx", "vitest", "run", "--reporter=dot").Output() // want: stop-hook-free-of-dead-js
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(string(out))
}
