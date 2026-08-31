// scopeglobs.go — the class-1 scope-glob integrity arm: every declared
// scope.include / scope.exclude / except.paths glob judged on its own. Split
// from classify.go, which the 750-line full-repo file cap bounds; same package.
// perglob.go measures the globs, this file decides what the measurements mean.
package main

import (
	"fmt"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// isClassGuard reports whether a subtractive glob names a CLASS of path rather
// than one named file.
//
// This is the discriminating half of the exclude/except arm (#12178), and the
// corpus is what forced it. Of the 263 exclude/except globs matching nothing,
// 232 are wildcards — `**/node_modules/**`, `**/*.pbjson.dart`,
// `**/.dart_tool/**`, `**/gen/go/**`, `.agent-work/**`, `.claude/worktrees/**` —
// naming a class of path that materialises whenever a build runs, a proto
// variant is generated, or an agent opens a scratch tree. For those, matching
// nothing TODAY is the guard holding, not the guard rotting: the exclude exists
// so the rule stays correct when the class appears, and deleting it is a
// regression rather than a fix. Eleven of them are the `**/node_modules/**`
// case the census's own note cited as the reason not to gate at all.
//
// The subtractive half of a scope is not symmetric with the additive half. A
// dead scope.include OVER-CLAIMS coverage the rule does not have — that is why
// EMPTY-GLOB gates. A dead exclude claims nothing; it is a guard, and an
// unfired guard is not a dead guard. What IS stale is a subtraction naming one
// literal path that no longer exists: a carve-out for a named file is a
// statement about that file, and all 31 in this corpus proved to be drift —
// 29 naming a scripts/check-*.sh gate formwork replaced and deleted, one a Dart
// provider that moved, one a proto that moved.
func isClassGuard(glob string) bool {
	return strings.ContainsAny(glob, "*?[")
}

// perGlobVerdicts is the per-glob arm (#10626). The whole-rule predicate
// (Applies) cannot say WHICH include glob earned a match, so a rule with one
// live glob and five dead ones scores healthy on every whole-rule instrument.
// Here each declared glob is matched on its own (see perglob.go):
//
//   - EMPTY-GLOB (class 1, GATING) — a scope.include glob matches no file
//     while at least one sibling glob is live. That is the shape a refactor
//     leaves behind: the moved path kills one glob, the tree glob keeps the
//     rule looking covered. A rule with NO live include glob is EMPTY-SCOPE's
//     finding, not this arm's, so the two never double-count.
//   - DEAD-EXCLUDE-GLOB / DEAD-EXCEPT-GLOB (GATED for literal-path carve-outs)
//     — a dead exclude over a named file is a statement about that file; when
//     the file is gone the carve-out reads as live while excusing nothing, so
//     the instrument fails closed. Class-guard globs (**/node_modules/** and
//     friends via isClassGuard) stay measured-not-gated so defensive entries
//     do not fail the build. Header and instrument must agree (#12382 / I14).
//
// A glob declared dead in place (`# glob-dead: <reason>` on the line above)
// is exempt from every arm — the reviewed escape hatch, never an allowlist.
func perGlobVerdicts(r *config.Rule, gm *globMeasure) []verdict {
	var out []verdict

	includes := gm.include[r.ID]
	live := 0
	for _, g := range includes {
		if g.n > 0 {
			live++
		}
	}
	if live > 0 {
		// A type:command rule's scope is a change-trigger hint, not
		// enforcement (command.go FinalizeErr runs it unconditionally), so a
		// dead glob there misleads rather than unguards. It still gates — the
		// glob states an intent it does not deliver — but the why says which.
		note := ""
		if rules.CostOf(r.Checker) == rules.CostHeavy {
			note = " — type:command, so scope is a change-trigger hint rather than enforcement, " +
				"but the glob still states an intent it does not deliver"
		}
		for _, g := range includes {
			if g.n > 0 || g.deadOK {
				continue
			}
			out = append(out, verdict{
				class: class1Glob, code: "EMPTY-GLOB", gating: true,
				why: fmt.Sprintf("scope.include glob %q matches no file — PARTIAL (%d/%d include globs live)%s",
					g.glob, live, len(includes), note),
			})
		}
	}

	for _, g := range gm.exclude[r.ID] {
		if g.n > 0 || isClassGuard(g.glob) {
			continue
		}
		out = append(out, verdict{
			class: class1Glob, code: "DEAD-EXCLUDE-GLOB", gating: true,
			why: "scope.exclude names a literal path that is not in the repository — a carve-out for one " +
				"named file is a statement about that file, and the file is gone. Delete the entry, or " +
				"repoint it at where the subject moved to",
			evidence: []string{g.glob},
		})
	}
	for _, g := range gm.except[r.ID] {
		if g.n > 0 || isClassGuard(g.glob) {
			continue
		}
		out = append(out, verdict{
			class: class1Glob, code: "DEAD-EXCEPT-GLOB", gating: true,
			why: "except.paths names a literal path that is not in the repository — the reviewed exception " +
				"reads as live while excusing nothing",
			evidence: []string{g.glob},
		})
	}
	return out
}

// exceptInertVerdicts is the second way an except.paths entry stops excusing
// anything (#10777). DEAD-EXCEPT-GLOB above catches the entry whose path is
// GONE. This catches the entry whose path is still THERE — present, scanned,
// in the rule's scope — and which the rule no longer fires on.
//
// That is the same rot one step later, and it is invisible to every other
// instrument precisely because except.paths subtracts from the scope: the rule
// never evaluates the file, so no finding is ever produced, let alone
// suppressed, and formwork lint's escape-hatch census keeps printing the entry
// as a live, accounted-for exception forever. dart-measure-active-step claimed
// measure_type_filter_providers.dart as the home of a resolver #10346 had
// turned into a projection, and the only thing that ever noticed was a human
// running the rule's own regex across two revisions.
//
// The cost is governance rather than tidiness. An inert entry makes an honest,
// correctly-reasoned amendment to an ownership manifest indistinguishable from
// an allowlist widening, because the list no longer says which entries are
// load-bearing.
//
// Scoped to LITERAL paths, exactly like the DEAD arm, and for the same reason
// (isClassGuard): a wildcard entry names a CLASS of path, and asking whether a
// class guard "excuses something today" is the question the subtractive half of
// a scope is not supposed to have to answer.
//
// Two rule shapes are not admitted, both already the census's own boundaries: a
// heavy (type:command) rule, whose verdict would mean shelling out once per
// entry and whose scope is a change-trigger hint rather than enforcement, and a
// relation obligation, which a one-file probe misreads by construction (a
// relation is satisfied by an empty tree).
func exceptInertVerdicts(r *config.Rule, root string, gm *globMeasure, byPath map[string]*scan.File) ([]verdict, error) {
	if !exceptProbeCanJudge(r) {
		return nil, nil
	}
	var out []verdict
	for _, g := range answerableExceptGlobs(gm, r.ID) {
		f, scanned := byPath[g.glob]
		if !scanned {
			// Counted live against walkIncludingBuiltinSkip, absent from the
			// engine's own walk: the path is on disk under .git or .formwork,
			// which every rule is dropped from before it runs. Named as its own
			// shape because the cure is different — nothing about the rule or
			// the file changes that.
			out = append(out, verdict{
				class: class1Glob, code: "INERT-EXCEPT", gating: true,
				why: "except.paths names a path the engine never reads — it is on disk but inside a " +
					"built-in walk skip (.git, .formwork), so no rule was ever going to fire on it and " +
					"the exception excuses nothing. Delete the entry",
				evidence: []string{g.glob},
			})
			continue
		}
		fires, err := exceptExcuses(r, root, f)
		if err != nil {
			return nil, fmt.Errorf("rule %s: probing except.paths %s: %w", r.ID, g.glob, err)
		}
		if fires {
			continue
		}
		why := "except.paths names a file that is present and in the rule's scope, but the rule does " +
			"not fire on it even with the exception removed — the entry excuses nothing while reading " +
			"as a live escape hatch. Delete it, repoint it at where the subject moved to (and check " +
			"whether the repointed rule now fires — that is a real latent violation, not a formatting " +
			"fix), or declare it in place with `# except-declaration: <reason>` when the file is the " +
			"subject's declaration home and can never be a violator"
		if !ruleAdmits(r, g.glob) {
			why = "except.paths subtracts a file the rule's scope never contained — scope.include does " +
				"not match it, or scope.exclude already removed it — so the exception has nothing to " +
				"subtract. Delete the entry, or fix the scope it was written against"
		}
		out = append(out, verdict{
			class: class1Glob, code: "INERT-EXCEPT", gating: true,
			why: why, evidence: []string{g.glob},
		})
	}
	return out, nil
}

// exceptProbeCanJudge reports whether the one-file INERT-EXCEPT probe is valid
// for r at all. A heavy (type:command, git-diff) rule would mean shelling out
// once per entry, and its scope is a change-trigger hint rather than
// enforcement; a relation obligation is satisfied by an EMPTY tree, so "the
// rule does not fire on this file alone" says nothing about it. Both are the
// census's own existing boundaries (isRelationObligation, CostOf).
//
// Named once, and paired with undecidedExceptEntries below, so the population
// the arm judges and the population it reports as undecided cannot drift apart
// — a blind spot that stops being counted is a blind spot that reads as
// coverage.
func exceptProbeCanJudge(r *config.Rule) bool {
	return rules.CostOf(r.Checker) != rules.CostHeavy && !isRelationObligation(r)
}

// answerableExceptGlobs returns the except.paths entries INERT-EXCEPT is
// answerable for: a LITERAL path (isClassGuard's split — a class guard names a
// class of path, and asking whether it excuses something today is the question
// a subtraction is not supposed to have to answer), present in the tree (a
// missing one is DEAD-EXCEPT-GLOB's finding, never this arm's), and not
// declared in place.
func answerableExceptGlobs(gm *globMeasure, ruleID string) []globCount {
	var out []globCount
	for _, g := range gm.except[ruleID] {
		if g.n == 0 || isClassGuard(g.glob) || g.declOK {
			continue
		}
		out = append(out, g)
	}
	return out
}

// undecidedExceptEntries counts the except.paths entries this arm declines to
// judge, so report() can say so out loud instead of printing a zero that reads
// as coverage. It is the same obligation main.go already discharges for the
// set-relations the blank-the-b-side experiment cannot answer, and it exists
// for the reason stated there: a hole that goes unnamed is credited as
// coverage, which is the defect the census exists to catch, one level up.
func undecidedExceptEntries(cfg *config.Config, gm *globMeasure) int {
	n := 0
	for _, r := range cfg.Rules {
		if exceptProbeCanJudge(r) {
			continue
		}
		n += len(answerableExceptGlobs(gm, r.ID))
	}
	return n
}

// ruleAdmits reports whether path is in r's scope once the except.paths
// subtraction is lifted — include matches and exclude does not. It separates
// "the rule cannot see this file" from "the rule sees it and does not fire",
// which have different cures.
func ruleAdmits(r *config.Rule, path string) bool {
	lifted := *r
	lifted.ExceptPaths = nil
	return lifted.Applies(path)
}
