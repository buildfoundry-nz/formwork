// formwork-pair-unit-census — the count-blindness census for pair-consistency
// arms (#12249).
//
// Usage: go run -C tools/formwork-pair-unit-census . <repo-root>
//
// Product enforcement is formwork type:command
// (.formwork/rules/pair-consistency-not-for-countable-obligations.yaml) with
// origin on this file.
// Exit 0 = no judged arm offends, 1 = offenders listed, 2 = usage/env error.
//
// # The defect
//
// pair-consistency with `where: same-file` (the default when `where:` is
// omitted) and the default presence obligation reports a file only when the
// companion is absent from it ENTIRELY: the first trigger in a file buys the
// companion, and every later trigger in that file is free (#12181). When the
// tree itself demonstrates the obligation is countable — one in-scope file
// carrying more than one trigger line — presence is the wrong semantics for
// that arm, and the arm must move to a per-occurrence unit
// (`where: same-func`) or a counted one (`obligation: countable`).
//
// Pass condition: every judged arm has at most one trigger line per in-scope
// file that has a per-unit predicate (.go, .dart, .proto). NO ALLOWLIST, NO
// CEILING, NO DECLARED EXCEPTION: the census fails on every offending arm
// in the corpus today, not just on the next one.
//
// # The judged population, and the per-file domain
//
// Judged: `type: pair-consistency` arms that OMITTED `where:` (the engine
// defaults to same-file) and whose obligation is presence (omitted or
// `presence`). The omitted default is the trap (#12181). An explicit
// `where: same-file` is a design act — explain-gate's header forbids both
// same-func and same-dir — the same class as `where: same-dir`, not a
// forgotten conversion. An arm at `where: same-func` already owes a companion
// per function, and an arm with `obligation: countable` owes
// count(requires) >= count(trigger) per unit — both are per-occurrence by
// construction.
//
// The domain predicate is PER FILE — "the offending file has a per-unit
// predicate" — measured at the file, never at the arm's glob list. The first
// draft judged arms that declared any `*.go` include glob (hasGoInScope),
// which admitted scan-warning-copy-names-the-fix on the strength of one Go
// glob while every multi-trigger file it carries is Dart. formwork#168 added
// Dart and proto same-func units, so those languages are in domain; shell
// is not. A .go/.dart/.proto file with more than one trigger line IS judged
// wherever it lives, mixed-scope arm or not.
//
// # Out of domain, by name — an engine limitation, not a waiver
//
// Shell evidence stays out of domain: there is no per-unit predicate for
// .sh, and converting go-compile-gate-disk-probe to same-func would take it
// from count-blind to totally blind (#12195 work item 4). That arm declares
// where: same-file as a signed unit. dart-sse-consumers-use-reconnect-loop
// carries at most one trigger line per file. This list is prose the detector
// never reads — the domain test runs per file.
//
// # Why an external tool
//
// The engine's scanner skips `.formwork/` outright
// (formwork's internal/scan/scan.go), so no declarative rule can read the
// rule corpus — a `forbidden-pattern` scoped at `.formwork/rules/**` matches
// zero files and passes vacuously forever, the exact defect this file exists
// to catch. Same reason formwork-rules-not-vacuous,
// count-relation-arm-is-anchored and the arm census all shell out to a tool.
// The measurement runs through the engine's OWN config loader, scope matcher,
// preprocess registry and line splitting (config.Load + scan.Walk +
// Rule.Applies + File.Variant), so a census verdict and a gate verdict cannot
// disagree — a hand-rolled scope matcher is what produced the false
// "133 of 200 rules match zero files" result the vacuity census exists to
// correct (#10083).
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// offender is one flagged arm plus the human-readable detail of its verdict.
type offender struct {
	path string
	line int
	rule string
	why  string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run -C tools/formwork-pair-unit-census . <repo-root>")
		os.Exit(2)
	}
	os.Exit(run(os.Args[1], os.Stdout, os.Stderr))
}

func run(root string, out, errOut io.Writer) int {
	// Calibrate the instrument before reporting on the corpus (calibrate.go).
	// A census whose verdict machinery is broken reports "0 flagged" and reads
	// exactly like a clean tree — and the calibration is corpus-independent, so
	// it still fires on the mutation-proof scratch whose corpus is pruned to
	// the one command rule.
	if err := calibrate(out); err != nil {
		fmt.Fprintln(errOut, "pair-unit census:", err)
		return 2
	}
	arms, err := loadCorpus(root)
	if err != nil {
		fmt.Fprintln(errOut, "pair-unit census:", err)
		return 2
	}
	bad, examined, err := detect(root, arms)
	if err != nil {
		fmt.Fprintln(errOut, "pair-unit census:", err)
		return 2
	}
	fmt.Fprintf(out, "pair-unit census: %d arm(s) in corpus, %d judged, %d flagged\n",
		len(arms), examined, len(bad))
	sort.Slice(bad, func(i, j int) bool {
		if bad[i].path != bad[j].path {
			return bad[i].path < bad[j].path
		}
		return bad[i].line < bad[j].line
	})
	for _, b := range bad {
		fmt.Fprintf(out, "FAIL %s:%d: arm %q is count-blind at file grain — %s\n", b.path, b.line, b.rule, b.why)
	}
	if len(bad) > 0 {
		fmt.Fprintf(out, "pair-consistency-not-for-countable-obligations: %d offending arm(s)\n", len(bad))
		return 1
	}
	fmt.Fprintf(out, "pair-consistency-not-for-countable-obligations: OK\n")
	return 0
}
