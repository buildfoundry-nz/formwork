//go:build ignore

package main

import (
	"os"
	"os/exec"
)

// check-widget-compiles — builds the widget package to prove it compiles.
func main() {
	if err := exec.Command("go", "build", "./...").Run(); err != nil { // want: go-compile-gates-source-disk-probe
		os.Exit(1)
	}
}
