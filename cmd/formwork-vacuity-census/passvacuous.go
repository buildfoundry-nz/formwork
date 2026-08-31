// passvacuous.go — PASS-FIXTURE-VACUOUS (#15986).
//
// The census asks whether a FIRE fixture still trips its rule, and whether a
// PASS fixture has started tripping it. It never asked the third question:
// could this PASS fixture EVER have tripped it? A pass fixture holding no file
// the rule would judge is green whatever the rule says, so it demonstrates
// nothing — the pass side of the same defect the fire side has always gated.
//
// Split out of classify.go, which reached 762 lines against the 750 cap. These
// three functions are one subject and carry their own test file
// (passvacuous_test.go), so they move together rather than being trimmed.

package main

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// passVacuousVerdict answers, of a clean fixture that did NOT fire, whether it
// could ever have fired (#15986).
//
// The census already asks the fire half of this — fireInScope is counted inside
// `if isFire` — and asked nothing of the pass half, so a clean fixture with no
// file in the rule's scope reported as a discriminating pair while being green
// whatever the rule said. On the corpus that introduced this arm, 113 of 2468
// pass fixtures had zero in-scope files.
//
// "No in-scope file" is NOT the offence, and that is the whole difficulty.
// Parking a real violation at an excluded path is how an exclusion glob is held
// in place: delete the glob and the fixture goes red, which is exactly what it
// exists to catch. 95 of the 113 were that. The offence is the conjunction —
// the rule reaches nothing here, AND there is nothing here it would have
// judged.
//
// The second half is decided by the ENGINE. The probe rebuilds the rule with a
// scope that subtracts nothing and re-runs it over the same tree, so the
// question becomes "does this rule judge anything in this tree" instead of
// "does this rule reach this tree". Deciding it that way is what makes POLARITY
// fall out for free rather than being hand-written: a ban is violated by a
// match, a required-pattern by the ABSENCE of one, a set-relation by a
// membership failure, and engine.Run already knows which is which. A
// hand-written "does the fixture contain the pattern" probe gets the
// required-pattern case backwards — it accuses precisely the fixture whose job
// is to hold an include glob's suffix in place by being an out-of-scope file
// that legitimately lacks the pattern. Re-implementing the matcher here is also
// the exact error that produced the false "133 of 200 rules match zero files"
// result this census exists to correct (#10083).
//
// HEAVY RULES ARE OUT OF CLASS BY REASON, not by exception. A type:command or
// git-diff checker runs its tool at the tree root and never consults scope for
// its verdict, so "no in-scope file" implies nothing about whether that fixture
// can discriminate, and the universal-scope probe would re-run an expensive
// tool to learn what the run three lines above already reported.
func passVacuousVerdict(r *config.Rule, fixture string, ffs *scan.FileSet, gm *globMeasure) ([]verdict, error) {
	if rules.CostOf(r.Checker) == rules.CostHeavy {
		return nil, nil
	}
	for _, ff := range ffs.Files {
		if r.Applies(ff.Path()) {
			return nil, nil // the rule reaches this fixture; it is doing its job
		}
	}
	if declaredInertExcept(r, ffs, gm) {
		return nil, nil // inert by written declaration, not by drift
	}
	judged, err := judgesAnything(r, ffs)
	if err != nil {
		return nil, fmt.Errorf("rule %s fixture %s: universal-scope probe: %w", r.ID, fixture, err)
	}
	if judged {
		return nil, nil // out of scope but carrying a real violation: a scope test
	}
	return []verdict{{
		class: class3, code: "PASS-FIXTURE-VACUOUS", gating: true,
		why: "the clean fixture has no file this rule reaches and nothing this rule would have judged — " +
			"it is green under any pattern and any scope, so it demonstrates nothing",
		evidence: []string{".formwork/fixtures/" + r.ID + "/" + fixture},
	}}, nil
}

// declaredInertExcept reports whether this fixture stands at an except.paths
// entry its author has DECLARED inert in place with `# except-declaration:
// <reason>` (#10777).
//
// That vocabulary already means "this entry names a file the pattern could
// never match anyway, listed so the roster stays complete and auditable" —
// labour-method-choice-single-writer says exactly that about catalog_labour.go,
// which is generic over every line_kind and carries both line_kind and
// subcategory_id as bind parameters, so no literal the pattern matches can
// appear in it. Firing there would demand the author delete an auditable roster
// entry or fabricate a violation to keep it, and an arm that does that is
// switched off within a week.
//
// The declaration must carry a non-empty reason: globMeasure sets declOK only
// for a marker with text after the colon, so a bare `# except-declaration:`
// buys nothing here, exactly as it buys nothing for the sibling vocabularies.
func declaredInertExcept(r *config.Rule, ffs *scan.FileSet, gm *globMeasure) bool {
	if gm == nil {
		return false
	}
	for _, gc := range gm.except[r.ID] {
		if !gc.declOK {
			continue
		}
		for _, ff := range ffs.Files {
			if ok, _ := doublestar.Match(gc.glob, ff.Path()); ok {
				return true
			}
		}
	}
	return false
}

// judgesAnything reports whether r finds anything to say about fs once its
// scope subtracts nothing. The verdict comes from engine.Run, never from a
// re-implementation of the matcher, and from a Fresh checker because a rule
// carries per-run state (required-pattern exists mode accumulates across
// files) — the same discipline as satisfies() in probe.go.
//
// Preprocess and Marker are carried across because they change what the checker
// SEES; Allowlist is dropped because its entries are repo-relative and a
// fixture tree has its own path namespace, the same collision EvalIn and
// satisfies() clear it for.
func judgesAnything(r *config.Rule, fs *scan.FileSet) (bool, error) {
	fresh, err := r.Fresh()
	if err != nil {
		return false, err
	}
	probe, err := config.New(r.ID, r.Type, r.Severity, r.Cure, []string{"**"}, nil, nil, fresh.Checker)
	if err != nil {
		return false, err
	}
	probe.Preprocess = r.Preprocess
	probe.Marker = r.Marker
	probe.Allowlist = nil
	fds, err := engine.Run([]*config.Rule{probe}, fs, 1)
	if err != nil {
		return false, err
	}
	return len(finding.Unsuppressed(fds)) > 0, nil
}
