//go:build ignore

// Reproduces check-routes-log-gate-always-runs.sh (#4026), ported to Go, on
// the fixture tree: the ci.yml job whose step invokes
// check-routes-log-pre-500.sh must be guarded by `diff_scope != 'docs'` and
// must NOT be gated on go_touched.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	jobKeyRe = regexp.MustCompile(`^  [A-Za-z0-9_-]+:[[:space:]]*$`)
	stepsRe  = regexp.MustCompile(`^    steps:[[:space:]]*$`)
)

func main() {
	const ci = ".github/workflows/ci.yml"
	const gate = "check-routes-log-pre-500.sh"
	data, err := os.ReadFile(ci)
	if err != nil {
		fmt.Printf("no %s\n", ci)
		os.Exit(0)
	}

	// Header region (job key through the line before `steps:`) of the job whose
	// steps invoke the gate. A comment mentioning the gate never counts: a
	// commented-out invocation is not an invocation, and reading one as the
	// wiring lets the meta-lock pass over a gate that never runs.
	//
	// The header region itself keeps its comment lines deliberately. Commenting
	// out a job's `if:` removes a condition, so the job runs MORE often, never
	// less; only the invocation match has a direction in which a comment can
	// hide the gate.
	header := ""
	instep := false
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if jobKeyRe.MatchString(line) {
			header = line
			instep = false
			continue
		}
		if stepsRe.MatchString(line) {
			instep = true
		}
		if !instep {
			header += "\n" + line
		}
		// TrimSpace, not a bare prefix test: a YAML comment carries the
		// indentation of the step it replaced.
		if !strings.HasPrefix(strings.TrimSpace(line), "#") &&
			strings.Contains(line, gate) &&
			(strings.Contains(line, "run:") || strings.Contains(line, "bash")) {
			found = true
			break
		}
	}

	if !found {
		fmt.Fprintf(os.Stderr, "ci.yml never invokes %s\n", gate)
		os.Exit(1)
	}

	if !strings.Contains(header, "diff_scope != 'docs'") {
		fmt.Fprintf(os.Stderr, "%s does not run in the always-run (diff_scope != 'docs') job\n", gate)
		os.Exit(1)
	}

	if strings.Contains(header, "go_touched") {
		fmt.Fprintf(os.Stderr, "%s is gated on go_touched\n", gate)
		os.Exit(1)
	}
	os.Exit(0)
}
