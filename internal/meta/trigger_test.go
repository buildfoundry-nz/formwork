// trigger_test.go — the command-trigger-armable check (#161). Separate from
// lint_test.go, which the 750-line vendor cap bounds; same package.
package meta_test

import (
	"strings"
	"testing"
)

// The reproducing config from #161: the checker is only reached for files that
// pass the rule's own scope, so a trigger that cannot intersect that scope
// arms nothing, in any mode, on any commit, forever. lint reported it healthy.
const deadGateRule = "rules:\n" +
	"  - id: dead-gate\n" +
	"    type: command\n" +
	"    scope: {include: ['src/**']}\n" +
	"    params:\n" +
	"      cmd: [\"false\"]\n" +
	"      when: {paths_changed: ['db/**']}\n" +
	"    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n"

func TestLintFailsCommandRuleWhoseTriggerCannotIntersectItsScope(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  deadGateRule,
		"src/main.go":             "package main\n",
		"db/0001.sql":             "select 1;\n",
	})
	if failed != 1 {
		t.Fatalf("a gate no path can arm must fail lint: failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[command-trigger-armable] FAIL — 1 problem(s)",
		"dead-gate",
		"src/**",
		"db/**",
		"cannot intersect",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The control #161 names: widen the scope and the SAME rule must go green,
// because the gate is then live. Without it a check that simply failed every
// when: rule would pass the test above vacuously.
func TestLintPassesCommandRuleWhoseTriggerIsArmable(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: live-gate\n" +
			"    type: command\n" +
			"    scope: {include: ['**']}\n" +
			"    params:\n" +
			"      cmd: [\"false\"]\n" +
			"      when: {paths_changed: ['db/**']}\n" +
			"    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n",
		"src/main.go": "package main\n",
		"db/0001.sql": "select 1;\n",
	})
	if failed != 0 {
		t.Fatalf("an armable gate must stay green: failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[command-trigger-armable] OK") {
		t.Fatalf("missing OK verdict:\n%s", out)
	}
}

// The third verdict, following prefilter-load-bearing's precedent (#133): the
// globs CAN intersect, so nothing proves the gate permanently dead — but no
// file in this repository satisfies both, so nothing here can arm it either.
// Reported as unproven, in its own words, never as "cannot intersect".
func TestLintReportsUnprovenWhenNoFileArmsAnIntersectableTrigger(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: absent-corpus-gate\n" +
			"    type: command\n" +
			"    scope: {include: ['**']}\n" +
			"    params:\n" +
			"      cmd: [\"false\"]\n" +
			"      when: {paths_changed: ['db/**']}\n" +
			"    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n",
		"src/main.go": "package main\n",
	})
	if failed != 1 {
		t.Fatalf("a trigger no file in the repo arms must fail lint: failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "unproven") {
		t.Fatalf("an intersectable-but-unarmed trigger must read as unproven:\n%s", out)
	}
	if strings.Contains(out, "cannot intersect") {
		t.Fatalf("a possible intersection must not be reported as an impossible one:\n%s", out)
	}
}

// A trigger whose literal prefix cannot be decided statically (`**/*.sql`) is
// exactly the case #161 warns a naive glob comparison gets wrong in the FAIL
// direction. It intersects `db/**` at db/0001.sql, and the check must find it.
func TestLintPassesWildcardTriggerThatIntersectsScopeOnlyThroughARealFile(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: sql-gate\n" +
			"    type: command\n" +
			"    scope: {include: ['db/**']}\n" +
			"    params:\n" +
			"      cmd: [\"true\"]\n" +
			"      when: {paths_changed: ['**/*.sql']}\n" +
			"    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n",
		"db/0001.sql": "select 1;\n",
	})
	if failed != 0 {
		t.Fatalf("`**/*.sql` does intersect `db/**`: failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[command-trigger-armable] OK") {
		t.Fatalf("missing OK verdict:\n%s", out)
	}
}

// scope.exclude carves the trigger paths back out: the rule's checker is never
// handed db/0001.sql, so the gate is as dead as if the include had excluded it.
// Applies (scope ∧ ¬exclude ∧ ¬except.paths) is the predicate #161 names.
func TestLintFailsCommandRuleWhoseTriggerIsExcludedFromItsScope(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: excluded-gate\n" +
			"    type: command\n" +
			"    scope:\n" +
			"      include: ['**']\n" +
			"      exclude: ['db/**'] # deliberate: keeps the exclude live\n" +
			"    params:\n" +
			"      cmd: [\"false\"]\n" +
			"      when: {paths_changed: ['db/**']}\n" +
			"    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n",
		"src/main.go": "package main\n",
		"db/0001.sql": "select 1;\n",
	})
	if failed != 1 {
		t.Fatalf("an excluded trigger arms nothing: failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "excluded-gate") {
		t.Fatalf("the offending rule is not named:\n%s", out)
	}
}

// The check is conditional, like the lane checks: a command rule with no when:
// gate has nothing to judge, so the check must not appear at all — neither as a
// line nor in the denominator an operator reads as coverage.
func TestLintOmitsTriggerCheckWhenNoRuleDeclaresAWhenGate(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": lintRule +
			"  - id: ungated\n    type: command\n    scope: {include: ['**/*.go']}\n    params: {cmd: [true]}\n    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "command-trigger-armable") {
		t.Fatalf("the check must be absent when no rule declares a trigger:\n%s", out)
	}
	if !strings.Contains(out, "formwork lint: 5/5 checks passed") {
		t.Fatalf("denominator must not grow for a check that had nothing to judge:\n%s", out)
	}
}

// #161's second half: the census names a command rule as an escape hatch, but
// said nothing about the gate that may stop it from ever running.
func TestCensusQualifiesATriggerGatedCommandEscape(t *testing.T) {
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  deadGateRule,
		"src/main.go":             "package main\n",
		"db/0001.sql":             "select 1;\n",
	})
	want := "  dead-gate: command rule (external tool, heavy — NO firing proof: no fixtures); gated by when.paths_changed (db/**) — runs only when a file in this rule's scope matches"
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}
