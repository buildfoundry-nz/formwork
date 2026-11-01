// scopeglobs.go — the class-1 scope-glob integrity arm: every declared
// scope.include / scope.exclude glob judged on its own. Split
// from classify.go, which the 750-line full-repo file cap bounds; same package.
// perglob.go measures the globs, this file decides what the measurements mean.
package main

import (
	"fmt"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// isClassGuard reports whether a subtractive glob names a CLASS of path rather
// than one named file.
//
// This is the discriminating half of the exclude arm (#12178), and the
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
//   - DEAD-EXCLUDE-GLOB (GATED for literal-path carve-outs)
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
	return out
}
