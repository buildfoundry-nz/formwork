//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// install-git-hooks — node-free wiring.
func main() {
	run("git", "config", "core.hooksPath", ".husky")
	run("git", "config", "merge.regen-schema-snapshot.driver", "go run scripts/git-merge-schema-snapshot-driver.go %O %A %B %P")
	fmt.Println("git hooks wired")
}

func run(args ...string) {
	cmd := exec.Command(args[0], args[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "%v: %s\n", err, out)
		os.Exit(1)
	}
}
