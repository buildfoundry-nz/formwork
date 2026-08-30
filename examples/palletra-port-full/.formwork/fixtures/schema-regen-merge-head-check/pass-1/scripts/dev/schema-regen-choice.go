//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var (
	triggerGlobs  = []string{"proto/**", "buf.yaml", "buf.gen.yaml"}
	snapshotGlobs = []string{"proto/**", "buf.yaml", "buf.gen.yaml", "schema/out/**"}
)

// Command table: each git invocation the wiring rules pin is kept contiguous
// in ONE string, so the multi-word tokens stay matchable.
const (
	triggerDiffCmd  = "git diff --cached --quiet --diff-filter=ACMRD"
	mergeHeadCmd    = "git rev-parse -q --verify MERGE_HEAD"
	provenanceCmd   = "git merge-base --is-ancestor MERGE_HEAD origin/develop"
	snapshotDiffCmd = "git diff --cached --quiet MERGE_HEAD"
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
	// Nothing staged that could touch the generated schema output -> skip.
	if runQuiet(triggerDiffCmd, append([]string{"--"}, triggerGlobs...)...) {
		fmt.Println("none")
		os.Exit(0)
	}

	// Mid-merge, and the resolved schema snapshot matches a known-good ancestor:
	// treat it as a no-op for regen purposes.
	if runQuiet(mergeHeadCmd) {
		if runQuiet(provenanceCmd) {
			if runQuiet(snapshotDiffCmd, append([]string{"--"}, snapshotGlobs...)...) {
				fmt.Println("skip-merge-identical")
				os.Exit(0)
			}
		}
	}

	fmt.Println("regen")
}
