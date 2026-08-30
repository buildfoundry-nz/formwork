//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"

	"freightworks/scripts/lib/godiskheadroom"
)

// check-widget-compiles — builds the widget package to prove it compiles. The
// failure path classifies an out-of-disk compiler exit as an ENVIRONMENT
// failure (exit 2) through the shared headroom probe, never as a code verdict.
func main() {
	out, err := exec.Command("go", "build", "./...").CombinedOutput()
	if err != nil {
		if godiskheadroom.BelowFloor() {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, string(out))
		os.Exit(1)
	}
}
