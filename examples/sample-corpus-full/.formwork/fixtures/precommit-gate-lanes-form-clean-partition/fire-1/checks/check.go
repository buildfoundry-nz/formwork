//go:build ignore

// Property A of check-precommit-gate-lanes-form-clean-partition.sh, ported to
// Go and reproduced on the fixture tree: the DOCS/GO/DART/ALWAYS lanes are a
// clean partition and every listed gate resolves to a scripts/<name> file.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	const m = "scripts/gate-manifest.tsv"
	data, err := os.ReadFile(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no %s\n", m)
		os.Exit(1)
	}
	lines := strings.Split(string(data), "\n")

	// A.1 — no gate basename double-listed across lanes.
	counts := map[string]int{}
	for _, line := range lines {
		t := strings.TrimLeft(line, " \t")
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		counts[strings.SplitN(line, "\t", 2)[0]]++
	}
	var dupes []string
	for name, n := range counts {
		if n > 1 {
			dupes = append(dupes, name)
		}
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		fmt.Fprintf(os.Stderr, "gate double-listed across lanes: %s\n", strings.Join(dupes, "\n"))
		os.Exit(1)
	}

	// A.2 — every listed gate resolves to a script.
	for _, line := range lines {
		name := strings.SplitN(line, "\t", 2)[0]
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		info, err := os.Stat("scripts/" + name)
		if err != nil || !info.Mode().IsRegular() {
			fmt.Fprintf(os.Stderr, "listed gate has no script: %s\n", name)
			os.Exit(1)
		}
	}
	os.Exit(0)
}
