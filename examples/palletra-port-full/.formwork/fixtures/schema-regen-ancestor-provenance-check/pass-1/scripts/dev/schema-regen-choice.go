//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Command table: each git invocation the wiring rules pin is kept contiguous
// in ONE string, so the multi-word tokens stay matchable.
const (
	mergeHeadCmd  = "git rev-parse -q --verify MERGE_HEAD"
	provenanceCmd = "git merge-base --is-ancestor MERGE_HEAD origin/develop"
)

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
	if !runQuiet(mergeHeadCmd) {
		fmt.Println("none")
		os.Exit(0)
	}

	if runQuiet(provenanceCmd) {
		fmt.Println("skip-merge-identical")
	} else {
		fmt.Println("none")
	}
}
