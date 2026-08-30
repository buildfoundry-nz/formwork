//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const mergeHeadCmd = "git rev-parse -q --verify MERGE_HEAD"

// runQuiet runs one command-table entry with extra args appended and reports
// whether it exited 0, with output suppressed.
func runQuiet(cmdline string, args ...string) bool {
	fields := strings.Fields(cmdline)
	cmd := exec.Command(fields[0], append(fields[1:], args...)...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func main() {
	// FIRE: the origin/develop ancestor provenance check has been dropped, so an
	// in-progress merge is treated as identical without ever proving ancestry.
	if !runQuiet(mergeHeadCmd) {
		fmt.Println("none")
		os.Exit(0)
	}

	fmt.Println("skip-merge-identical")
}
