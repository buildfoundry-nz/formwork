// trigger.go — the command-trigger-armable check (#161). Split from lint.go,
// which the 750-line vendor cap bounds; same package.
package meta

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// triggerGated is the consumer-side view of a checker whose whole-run execution
// is gated on a path trigger. It is declared here rather than in internal/rules
// because lint is the only thing that asks: the registry publishes the contracts
// the ENGINE depends on, and a checker that grew a trigger without telling lint
// would be reported as ungated, which is the honest reading of a checker that
// does not answer the question.
type triggerGated interface {
	// TriggerGlobs returns the globs that must match a file the checker was
	// handed for the rule to run at all, or nil when the rule is ungated.
	TriggerGlobs() []string
}

// triggerGlobsOf returns c's whole-run trigger globs, if it has any. A checker
// that implements the interface but returns nothing is ungated, so callers never
// have to distinguish "no interface" from "no gate".
func triggerGlobsOf(c rules.Checker) ([]string, bool) {
	tg, ok := c.(triggerGated)
	if !ok {
		return nil, false
	}
	globs := tg.TriggerGlobs()
	if len(globs) == 0 {
		return nil, false
	}
	return globs, true
}

// anyTriggerGated reports whether any rule declares a whole-run path trigger.
// It is the cheap conditional the caller asks BEFORE consulting the skip list,
// so on a corpus whose rules declare no trigger the skip list is never asked
// about this check, the entry stays unseen, and lintPolicy.unusedErr reports it
// on a whole-corpus run. Asked the other way round the entry would be marked
// used, and the run would print a disclosure for a check that was never going to
// run. (Under `--rule` unusedErr is disarmed, because there the narrowing rather
// than the corpus is what left the check unarmed — see its comment.)
func anyTriggerGated(rls []*config.Rule) bool {
	for _, r := range rls {
		if _, ok := triggerGlobsOf(r.Checker); ok {
			return true
		}
	}
	return false
}

// commandTriggerProblems reports every trigger-gated rule that no file in this
// repository can arm.
//
// The engine hands a checker only the files that pass the rule's own scope, so
// the trigger is evaluated against `Applies` (scope ∧ ¬exclude ∧ ¬except.paths),
// not against the walk. A trigger that names paths the rule's scope carves out
// arms nothing however those paths were scanned — that is #161's whole shape,
// and it is why an `exclude:` that removes the trigger paths fails here just as
// a divergent `include:` does.
//
// Three verdicts, not two, following prefilter-load-bearing (#133):
//
//   - ARMABLE — a real file satisfies both. Green, and it stays green on a
//     commit that does not happen to touch that file: this check judges the
//     repository, never the changeset.
//   - DISJOINT — the globs provably cannot intersect, so no repository can arm
//     the gate. The strongest statement available, and it needs no files.
//   - UNPROVEN — the globs could intersect, but nothing in this tree does. This
//     is not "probably fine": the gate cannot fire here, so the rule guards
//     nothing in this repository today. It is reported in its own words because
//     the cure differs — a disjoint pair is a typo in the rule, an unproven one
//     is usually a corpus that has not been created yet.
//
// Both non-green verdicts are problems, which is what makes this the sibling of
// empty-scope rather than of the escape-hatch census: #161's acceptance
// criterion is that a gate no file in the repo can arm FAILS lint.
func commandTriggerProblems(rls []*config.Rule, files []*scan.File) (problems []string) {
	for _, r := range rls {
		trigger, ok := triggerGlobsOf(r.Checker)
		if !ok {
			continue
		}
		if armableBy(r, trigger, files) {
			continue
		}
		scope := r.Include()
		if provablyDisjoint(scope, trigger) {
			problems = append(problems, fmt.Sprintf(
				"%s: when.paths_changed (%s) cannot intersect scope.include (%s) — no path satisfies both, so the gate can never run, on any commit, in any repository; widen the scope or fix the trigger",
				r.ID, strings.Join(trigger, ", "), strings.Join(scope, ", ")))
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s: no file in this repository satisfies both scope.include (%s) and when.paths_changed (%s), so the gate is unproven — the globs could intersect, but nothing here arms them, and the tool guards nothing until something does; add the corpus it gates, widen the scope, or fix the trigger",
			r.ID, strings.Join(scope, ", "), strings.Join(trigger, ", ")))
	}
	return problems
}

// armableBy reports whether any scanned file both reaches r's checker and
// matches one of its trigger globs.
func armableBy(r *config.Rule, trigger []string, files []*scan.File) bool {
	for _, f := range files {
		p := f.Path()
		if !r.Applies(p) {
			continue
		}
		for _, g := range trigger {
			// Match's only error is ErrBadPattern, and both glob families were
			// validated at load (config.New for scope, newCommand for the
			// trigger), so the error branch is unreachable — the same reasoning
			// excludeMatchesAny records.
			if ok, err := doublestar.Match(g, p); err == nil && ok {
				return true
			}
		}
	}
	return false
}

// provablyDisjoint reports whether NO path can match both some glob in scope and
// some glob in trigger. It is deliberately ONE-SIDED: false means "not proven",
// never "they intersect".
//
// Deciding glob intersection in general is not glob matching, and #161 is
// explicit that a wrong answer in the FAIL direction — condemning a healthy rule
// — is worse than no answer at all. So this proves only the case it can prove
// soundly: every path a glob matches begins with that glob's literal directory
// prefix, so two globs whose prefixes diverge at any segment share no path. That
// covers the reported defect (`src/**` vs `db/**`) and declines the trap beside
// it (`**/*.sql` vs `db/**`, which has no literal prefix to compare and does
// intersect at db/0001.sql). Everything it declines falls through to the tree
// evidence, which reports unproven rather than passing.
//
// scope.exclude and except.paths are not consulted, and that omission is safe in
// this direction only: they can only REMOVE paths from the scope, so ignoring
// them can never turn a genuinely disjoint pair into an intersecting one. The
// tree arm above does honour them, through Applies.
func provablyDisjoint(scope, trigger []string) bool {
	if len(scope) == 0 || len(trigger) == 0 {
		return false
	}
	for _, s := range scope {
		sp := literalPrefixSegments(s)
		for _, t := range trigger {
			if !segmentsDiverge(sp, literalPrefixSegments(t)) {
				return false
			}
		}
	}
	return true
}

// globMeta is the set of characters doublestar reads as pattern syntax rather
// than as a literal path byte. A pattern is literal up to the first of these.
const globMeta = `*?[]{}\`

// literalPrefixSegments returns the path segments every path matching glob must
// begin with. A glob with no pattern syntax at all is literal to its leaf, so
// its whole path is the prefix; otherwise the prefix stops at the last complete
// segment before the first metacharacter, which is why `src/**` yields ["src"]
// and `**/*.sql` yields nothing.
func literalPrefixSegments(glob string) []string {
	i := strings.IndexAny(glob, globMeta)
	if i < 0 {
		return strings.Split(glob, "/")
	}
	cut := strings.LastIndexByte(glob[:i], '/')
	if cut < 0 {
		return nil
	}
	return strings.Split(glob[:cut], "/")
}

// segmentsDiverge reports whether two literal prefixes disagree on a segment
// they both have. Comparison is per SEGMENT, never per byte: `src` and `srcgen`
// share a byte prefix but name different directories, and a strings.HasPrefix
// test would call that pair overlapping. An empty prefix diverges from nothing,
// which is what makes an undecidable glob fall through to the tree arm.
func segmentsDiverge(a, b []string) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}
