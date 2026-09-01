package meta

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// prefilterLoadBearing enforces the prefilter contract (spec §5): a prefilter is
// a pure optimization, so evaluating a rule with it and without it must report
// the same thing. It reports on three kinds of evidence, because no single kind
// covers the fleet:
//
//  1. The real tree. Evaluate as-written vs prefilter-stripped over the repo and
//     diff. Catches a prefilter that narrows a rule with live findings.
//
//  2. The rule's own fixtures (#133). A tombstone rule has zero live findings by
//     construction — both sides of (1) are empty and it passes with no evidence
//     at all, which is most of a mature corpus. Its fire fixtures exist precisely
//     to contain what it bans, so they are the data the tree cannot supply.
//
//  3. The compiled pattern (#133). Fixtures only help where they exercise the
//     branch a wrong prefilter drops. Implication is a property of the pattern
//     alone, so it covers branches no fixture reaches — and needs no data.
//
// A rule with no evidence of any kind — no fixtures, and a pattern the static
// analysis declines — is REPORTED as unproven rather than passed. A check that
// cannot fail is a check that passes, which is the defect this whole check
// exists to prevent; it must not reproduce it one level up.
//
// ran is false when no rule carries a prefilter, so the caller skips the check
// like the lane checks. Any engine error is returned for the caller to surface
// as exit 2 — never folded into a comparison as agreement.
//
// When reuseMain is true, mainFindings is the engine's successful evaluation of
// cfg.Rules; the as-written (base) side of (1) is taken from it instead of a
// dedicated pass. A rule's findings are independent of which other rules run
// alongside it, so filtering mainFindings to the prefilter rule IDs is identical
// to a standalone base run — and avoids evaluating those rules a third time. The
// caller passes reuseMain=false when the main run was skipped OR errored, so a
// nil finding set from a failed run is never mistaken for an empty base.
func prefilterLoadBearing(cfg *config.Config, root string, fset *scan.FileSet, mainFindings []finding.Finding, reuseMain bool) (problems []string, ran bool, err error) {
	var stripped []*config.Rule
	var prefilterRules []*config.Rule
	prefilterID := map[string]bool{}
	literal := map[string]string{}
	for _, r := range cfg.Rules {
		if r.Library != "" {
			continue
		}
		lit, ok := rules.PrefilterOf(r.Checker)
		if !ok {
			continue
		}
		literal[r.ID] = lit
		prefilterID[r.ID] = true
		prefilterRules = append(prefilterRules, r)
		sc := r.Checker.(rules.Prefiltered).WithoutPrefilter()
		stripped = append(stripped, r.CloneWithChecker(sc))
	}
	if len(stripped) == 0 {
		return nil, false, nil
	}

	var baseFindings []finding.Finding
	if reuseMain {
		// Reuse the main run: keep only the prefilter rules' findings.
		for _, f := range mainFindings {
			if prefilterID[f.RuleID] {
				baseFindings = append(baseFindings, f)
			}
		}
	} else {
		// Main run was skipped — evaluate the prefilter rules as written.
		if baseFindings, err = engine.Run(prefilterRules, fset, 0); err != nil {
			return nil, true, err
		}
	}
	strippedFindings, err := engine.Run(stripped, fset, 0)
	if err != nil {
		return nil, true, err
	}

	// Removing a gate can only ADD matches, so a disagreement is always a
	// stripped finding absent from base — this one-directional diff is complete
	// ONLY because every rules.Prefiltered checker is per-file monotone
	// (forbidden, the sole implementer, gates a per-file scan; see the
	// interface's "stateless per-file checkers only" contract). A hypothetical
	// whole-tree-invariant Prefiltered checker (verdict on ABSENCE, e.g.
	// required-pattern exists-mode) could instead REMOVE a base finding when
	// stripped, which this diff would miss; such a checker would need a
	// bidirectional comparison. Compare full (rule,path,line) keys including
	// suppressed findings; report one line per (rule,path). Both slices are
	// already sorted by (rule,path,line) (engine.Run), so iterating stripped
	// yields deterministic (rule,path) order without re-sorting.
	proven := map[string]bool{} // rules a differential has already judged
	for _, p := range diffNewFindings(baseFindings, strippedFindings, literal, "") {
		proven[p.ruleID] = true
		problems = append(problems, p.text)
	}

	// Arm 2 + 3, per rule, in config order (deterministic without sorting).
	//
	// One rule earns one verdict. A rule the real-tree differential already
	// named is not re-diagnosed by the later arms: they would be restating the
	// same defect in weaker terms, and a check that prints two lines for one
	// fix trains readers to skim it. Concrete evidence — an actual file that
	// matches — outranks the abstract branch argument.
	for _, r := range prefilterRules {
		if proven[r.ID] {
			continue
		}
		fixtureProblems, ranFixtures, err := prefilterFixtureDifferential(r, root, literal[r.ID])
		if err != nil {
			return nil, true, err
		}
		if len(fixtureProblems) > 0 {
			problems = append(problems, fixtureProblems...)
			continue
		}
		implied, decidable, counterexample := rules.PrefilterImpliedBy(r.Checker)
		if decidable && !implied {
			// The pattern itself proves a branch can match without the literal.
			// This fires where no fixture reaches that branch, which is exactly
			// the gap fixtures leave.
			problems = append(problems, fmt.Sprintf(
				"%s: prefilter %q is load-bearing — %s can match without it, so the rule can never fire on that branch; move the scope to require_present: if intended.",
				r.ID, literal[r.ID], counterexample))
			continue
		}
		if ranFixtures || (decidable && implied) {
			continue // an arm had evidence, and it agreed
		}
		problems = append(problems, fmt.Sprintf(
			"%s: prefilter %q is unproven — the rule has no findings, no fixtures, and a pattern this check cannot analyse, so nothing here can tell a pure prefilter from one that silently narrows the rule. Add a fire fixture per alternative, or drop the prefilter.",
			r.ID, literal[r.ID]))
	}
	return problems, true, nil
}

// anyPrefiltered reports whether any rule carries a prefilter. It is the cheap
// conditional the caller asks BEFORE consulting the skip list, so on a corpus
// with no prefilters the skip list is never asked about this check, the entry
// stays unseen, and the dead-entry refusal reports it. Asked the other way
// round, the entry would be marked used and a whole-corpus run would disclose a
// skip for a check that was never going to run. prefilterLoadBearing returns the
// same fact as `ran`, but only after doing the work.
func anyPrefiltered(rls []*config.Rule) bool {
	for _, r := range rls {
		if _, ok := rules.PrefilterOf(r.Checker); ok {
			return true
		}
	}
	return false
}

type prefilterProblem struct {
	ruleID string
	text   string
}

// diffNewFindings reports findings present in stripped but not in base, one line
// per (rule, path). where names the fixture the comparison ran in, or "" for the
// real tree.
func diffNewFindings(base, stripped []finding.Finding, literal map[string]string, where string) []prefilterProblem {
	type fkey struct {
		rule, path string
		line       int
	}
	inBase := map[fkey]bool{}
	for _, f := range base {
		inBase[fkey{f.RuleID, f.Path, f.Line}] = true
	}
	type rp struct{ rule, path string }
	seen := map[rp]bool{}
	var out []prefilterProblem
	for _, f := range stripped {
		if inBase[fkey{f.RuleID, f.Path, f.Line}] {
			continue
		}
		id := rp{f.RuleID, f.Path}
		if seen[id] {
			continue
		}
		seen[id] = true
		in := ""
		if where != "" {
			in = " in fixture " + where
		}
		out = append(out, prefilterProblem{f.RuleID, fmt.Sprintf(
			"%s: prefilter %q is load-bearing — removing it makes the rule match %s%s; move the scope to require_present: if intended.",
			f.RuleID, literal[f.RuleID], f.Path, in)})
	}
	return out
}

// prefilterFixtureDifferential runs the same as-written vs stripped comparison
// over the rule's own fixture trees. ranFixtures reports whether any fixture
// existed to compare — the caller distinguishes "fixtures agreed" from "there
// was nothing to ask", because only the latter leaves the prefilter unproven.
//
// The walk goes through fixturetest.EvalIn so these runs inherit exactly what a
// `formwork test` fixture run inherits and no more: no repo allowlist, and no
// scan.ignore/scan.gitignore pruning. That matters here specifically — a
// repo-level prune would hide the fire fixture that proves the prefilter
// load-bearing, and the check would report a clean bill on the evidence it just
// discarded.
func prefilterFixtureDifferential(r *config.Rule, root, lit string) (problems []string, ranFixtures bool, err error) {
	ruleDir := filepath.Join(root, ".formwork", "fixtures", r.ID)
	entries, err := os.ReadDir(ruleDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("prefilter-load-bearing: reading %s: %w", ruleDir, err)
	}
	var names []string
	for _, e := range entries {
		// #143 row 4, lint's half. DirEntry.IsDir is lstat-based, so a symlinked
		// fixture tree has IsDir() false and used to drop out of this collection
		// in silence — leaving the differential to judge less evidence than the
		// corpus declares and print a verdict about the trees it happened to
		// enter. For a tombstone rule the fixture arm is the ONLY arm (#133), so
		// the missing tree can be the whole basis of the answer.
		//
		// Refused rather than followed, matching internal/fixturetest, which
		// makes the same refusal for the same reason. Both are needed: `lint`
		// and `test` are separate commands and an operator can run either alone.
		if e.Type()&fs.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("prefilter-load-bearing: %s is a symlink — refused: the fixture walk does not follow links, so this tree would be dropped from the differential and the verdict would rest on evidence that was never read; use a real directory",
				filepath.Join(ruleDir, e.Name()))
		}
		if !e.IsDir() {
			continue
		}
		if n := e.Name(); strings.HasPrefix(n, "fire-") || strings.HasPrefix(n, "pass-") {
			names = append(names, n)
		}
	}
	sort.Strings(names) // deterministic output regardless of directory order

	literal := map[string]string{r.ID: lit}
	for _, name := range names {
		dir := filepath.Join(ruleDir, name)
		fresh, err := r.Fresh()
		if err != nil {
			return nil, true, err
		}
		base, _, err := fixturetest.EvalIn(fresh, dir, 0)
		if err != nil {
			return nil, true, err
		}
		freshStripped, err := r.Fresh()
		if err != nil {
			return nil, true, err
		}
		sc := freshStripped.Checker.(rules.Prefiltered).WithoutPrefilter()
		strippedFindings, _, err := fixturetest.EvalIn(freshStripped.CloneWithChecker(sc), dir, 0)
		if err != nil {
			return nil, true, err
		}
		for _, p := range diffNewFindings(base, strippedFindings, literal, name) {
			problems = append(problems, p.text)
		}
		ranFixtures = true
	}
	return problems, ranFixtures, nil
}
