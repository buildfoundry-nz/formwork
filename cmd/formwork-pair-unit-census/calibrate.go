package main

import (
	"fmt"
	"io"
)

// Instrument calibration — the census proves its own verdict machinery before
// it reports on the corpus, the same way the arm census self-tests its probe
// and shape classifier before printing a population.
//
// It is not optional, for two reasons. FIRST, every failure mode of this check
// is silent: widen isJudgedArm and the census starts demanding per-occurrence
// units of arms whose grain is a deliberate design act (same-dir) — the one
// way it could do net harm — and narrow it and the census stops seeing the
// population it exists for, reporting OK forever. SECOND, it is what makes the
// rule PROVABLE: mutation-proof materialises the rule into a scratch corpus
// pruned to the one command arm, so the .formwork/rules/ this census reads
// there holds no pair-consistency arm at all, and no edit to the verdict logic
// can change that verdict. The calibration runs first and is
// corpus-independent, so a mutation that breaks the instrument fails the gate
// wherever it runs.

// judgedCalibration pins isJudgedArm on the shapes the census has to tell
// apart. The same-dir row is the load-bearing one: a package-collective unit
// is a deliberate design act (#12249 section B), and flagging it would demand
// a `where:` edit that fires on correct code — a rule with that false-positive
// rate gets disabled, which is worse than the count-blindness it replaced.
var judgedCalibration = []struct {
	a    arm
	want bool
	why  string
}{
	{arm{Type: "pair-consistency"}, true, "where omitted defaults to same-file; obligation omitted is presence"},
	{arm{Type: "pair-consistency", Where: "same-file"}, false, "explicit same-file is a design act (explain-gate: the capture is file-scoped); its mechanism is not this census's"},
	{arm{Type: "pair-consistency", Where: "same-func"}, false, "a per-function unit is per-occurrence already"},
	{arm{Type: "pair-consistency", Where: "same-dir"}, false, "a package-collective unit is a design act; its mechanism is #12239's"},
	{arm{Type: "pair-consistency", Obligation: "countable"}, false, "count(requires) >= count(trigger) per unit enforces the relation"},
	{arm{Type: "pair-consistency", Where: "same-file", Obligation: "countable"}, false, "countable at file grain is per-occurrence already"},
	{arm{Type: "required-pattern"}, false, "not a pair arm"},
	{arm{Type: "set-relation"}, false, "not a pair arm"},
}

// countBlindCalibration pins the verdict primitive on both its clauses: the
// count (>1 trigger line) and the per-file Go domain. The non-Go rows are the
// #12195 engine limitation, stated per file so a mixed-scope arm's Go evidence
// is judged while its Dart evidence waits for the engine's Dart unit.
var countBlindCalibration = []struct {
	n    int
	path string
	want bool
	why  string
}{
	{2, "internal/x/writer.go", true, "two trigger lines in one Go file is count-blind evidence"},
	{11, "routes/boq_templates/boq_templates_items.go", true, "the #12181 measured case"},
	{1, "internal/x/writer.go", false, "one trigger per file is the pass condition"},
	{0, "internal/x/writer.go", false, "no trigger, no obligation"},
	{5, "packages/feature_pricing/lib/providers/quote_actions_providers.dart", true, "Dart evidence is in domain now that same-func has a Dart unit (#12195)"},
	{8, "schema/proto/takeoffqs/domain/v1/priced_item.proto", true, "proto evidence is in domain now that same-func has a proto unit (#12195)"},
	{3, "scripts/check-go-staticcheck.sh", false, "shell evidence is out of domain (#12195)"},
	{2, "api-factory/internal/x/writer_test.go", true, "a _test.go file is Go — the domain is the language, not the file's role"},
}

// calibrate runs the self-test and reports it. A disagreement is an ERROR,
// never a quiet pass: the caller turns it into exit 2, so a broken instrument
// is distinguishable from a clean corpus.
func calibrate(out io.Writer) error {
	fmt.Fprintf(out, "pair-unit census: instrument calibration\n")
	for _, c := range judgedCalibration {
		if got := isJudgedArm(c.a); got != c.want {
			return fmt.Errorf("calibration FAILED: isJudgedArm(%+v) = %v, want %v — %s. The verdict machinery is broken; a corpus report from it would be meaningless", c.a, got, c.want, c.why)
		}
	}
	fmt.Fprintf(out, "  subject test%22s%d shape(s), omitted-where presence in, explicit same-file/same-func/same-dir/countable out — ok\n", "", len(judgedCalibration))
	for _, c := range countBlindCalibration {
		if got := countBlind(c.n, c.path); got != c.want {
			return fmt.Errorf("calibration FAILED: countBlind(%d, %q) = %v, want %v — %s. The verdict machinery is broken; a corpus report from it would be meaningless", c.n, c.path, got, c.want, c.why)
		}
	}
	fmt.Fprintf(out, "  verdict primitive%16s%d case(s), >1 trigger line in a Go/Dart/proto file flagged, shell spared — ok\n", "", len(countBlindCalibration))
	return nil
}
