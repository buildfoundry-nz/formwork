package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/dlclark/regexp2"
)

// witnessThreshold is how many independently sufficient matching lines make an
// existence obligation a LAST-WITNESS gate: a gate whose cure names a class but
// whose detector goes green on one surviving member of it.
//
// 12, matching the vacuity census's own diffuseThreshold
// (tools/formwork-vacuity-census/probe.go) — the same quantity, asked of a
// population that census cannot reach. Its DIFFUSE-EVIDENCE verdict is gated
// behind `len(ws) <= 3`, so a rule with four or more witness FILES is never
// asked the question at all, and the verdict is measured rather than gating
// even when it is asked. Reusing the number keeps one definition of "diffuse"
// in the repo rather than two that can drift apart.
const witnessThreshold = 12

// lineMatcher is the two-backend matcher the engine uses, re-expressed here
// because compileMatcher is package-private to internal/rules/pattern. The
// backends and the timeout are the same, so a count measured here is the count
// the gate would report.
// find returns the byte bounds of the leftmost match, or nil when there is
// none. narrowable.go needs WHERE a pattern stops, not merely whether it
// matched, and both backends already carry that fact.
type lineMatcher interface {
	matches(string) (bool, error)
	find(string) ([]int, error)
}

type re2Line struct{ re *regexp.Regexp }

func (m re2Line) matches(s string) (bool, error) { return m.re.MatchString(s), nil }

func (m re2Line) find(s string) ([]int, error) { return m.re.FindStringIndex(s), nil }

type pcreLine struct{ re *regexp2.Regexp }

func (m pcreLine) matches(s string) (bool, error) { return m.re.MatchString(s) }

func (m pcreLine) find(s string) ([]int, error) {
	mt, err := m.re.FindStringMatch(s)
	if err != nil || mt == nil {
		return nil, err
	}
	return []int{mt.Index, mt.Index + mt.Length}, nil
}

func compileLine(pattern, syntax string) (lineMatcher, error) {
	switch syntax {
	case "", "re2":
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
		return re2Line{re: re}, nil
	case "regexp2":
		re, err := regexp2.Compile(pattern, regexp2.None)
		if err != nil {
			return nil, fmt.Errorf("invalid regexp2 pattern %q: %w", pattern, err)
		}
		re.MatchTimeout = time.Second
		return pcreLine{re: re}, nil
	default:
		return nil, fmt.Errorf("unknown syntax %q (want re2 or regexp2)", syntax)
	}
}

// isLastWitnessShape reports whether the arm's verdict rests on ONE surviving
// witness: `required-pattern mode: exists`, or the degenerate counted arm
// `pattern-count op: at-least, n: 1`. Those are the two spellings of "somebody,
// somewhere, still does this".
//
// An explicit `n >= 2` is DELIBERATELY out of scope, and the distinction is the
// whole correctness of this check. `exists` is a default — the author chose no
// number, so the floor of one is an accident of the rule type. An explicit n is
// a stated cardinality: tqs-package-consumers-tqs-core is `at-least 2` over 42
// matching pubspec lines because the invariant IS "a tqs_* package earns its
// prefix at >= 2 consumers" (#6858), and its cure says exactly that. Demanding
// n == 42 there would fail the build every time a package legitimately dropped
// a dependency — a rule with that false-positive rate gets disabled, which is
// worse than the vacuity it replaced. Flagging a stated design threshold as if
// it were an accident is the one way this check could do net harm, so it does
// not.
//
// Everything else — forbidden-pattern, ceilings, relations, external commands —
// is out of scope for the same reason it is out of scope for the vacuity
// census's witness probes: deleting the subject SATISFIES those, so counting
// their witnesses answers nothing.
func isLastWitnessShape(a arm) bool {
	switch a.Type {
	case "required-pattern":
		return a.Mode == "exists"
	case "pattern-count":
		return a.Op == "at-least" && a.N == 1
	}
	return false
}

// detectMultiWitness flags every last-witness-shaped arm carrying at least
// witnessThreshold witnesses in the tree.
//
// Curing one is what removes it from the population, not from the count: a
// converted arm carries an explicit n >= 2 and is no longer last-witness
// shaped, so the check is idempotent by construction rather than by a
// remembered baseline.
func detectMultiWitness(root string, arms []arm) ([]offender, int, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*config.Rule, len(cfg.Rules))
	for _, r := range cfg.Rules {
		byID[r.ID] = r
	}
	fset, err := scan.Walk(root)
	if err != nil {
		return nil, 0, err
	}
	var bad []offender
	examined := 0
	for _, a := range arms {
		if !isLastWitnessShape(a) {
			continue
		}
		r, ok := byID[a.ID]
		if !ok {
			// The corpus reader saw an arm the engine did not load. That is a
			// loader disagreement, not a clean skip: reporting it as "nothing
			// to measure" is how a rule leaves a census unnoticed.
			return nil, 0, fmt.Errorf("%s:%d: arm %q is declared but the engine did not load it", a.File, a.Line, a.ID)
		}
		examined++
		m, err := compileLine(a.Pattern, a.Syntax)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d (%s): %w", a.File, a.Line, a.ID, err)
		}
		n, err := countWitnesses(r, m, fset.Files)
		if err != nil {
			return nil, 0, err
		}
		if n < witnessThreshold {
			continue
		}
		bad = append(bad, offender{a.File, a.Line, a.ID, fmt.Sprintf(
			"%d witnesses in scope, but the arm needs only 1 — %d could be deleted before it noticed. "+
				"Cure: `type: pattern-count` with `op: at-least, n: %d`. If %d is not the number the "+
				"invariant is about, narrow the pattern until it is (a count that includes comments or "+
				"unrelated matches is a floor on prose).", n, n-1, n, n)})
	}
	return bad, examined, nil
}

// countWitnesses tallies the matching lines of r's pattern across r's scope,
// read through r's own preprocess variant.
//
// The scope and the preprocessing come from the COMPILED rule, so the count is
// the one `pattern-count` would report for the same pattern — which is what
// makes the cure ("convert to pattern-count at-least N") mechanically true
// rather than an estimate. It is counted in full rather than stopped at the
// threshold for exactly that reason: the number the author has to write is the
// output of this function, so a report that stopped at twelve would send them
// back to measure it by hand.
func countWitnesses(r *config.Rule, m lineMatcher, files []*scan.File) (int, error) {
	total := 0
	for _, f := range files {
		if !r.Applies(f.Path()) {
			continue
		}
		v, err := f.Variant(r.Preprocess)
		if err != nil {
			return 0, fmt.Errorf("%s: %s: %w", r.ID, f.Path(), err)
		}
		lines, err := v.Lines()
		if err != nil {
			return 0, fmt.Errorf("%s: %s: %w", r.ID, f.Path(), err)
		}
		for _, l := range lines {
			ok, err := m.matches(l)
			if err != nil {
				return 0, fmt.Errorf("%s: %s: %w", r.ID, f.Path(), err)
			}
			if ok {
				total++
			}
		}
	}
	return total, nil
}
