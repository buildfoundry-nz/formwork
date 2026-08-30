package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// #151 rows 10 and 12: a run with NO RULES TO RUN reports "0/0 rules passed,
// 0 finding(s)" at exit 0 — a green verdict over an engine that evaluated
// nothing. It is reachable three ways (no rules configured, a lane that selects
// none, --skip-escapes dropping the last one), and today all three are silent.
//
// The contract these pin: zero rules to run is a CONFIG ERROR (exit 2), and the
// message names which of the three causes produced it. This mirrors the
// selector that is already fail-closed one line away — an UNKNOWN lane is exit 2
// (TestCheckUnknownLaneExits2), while a lane resolving to nothing was exit 0.
// A selector that names no rules and a selector that names no EXISTING rules are
// the same hazard.
//
// The repo's own precedents for refusing a vacuous run rather than reporting it
// green: internal/publication/manifest.go ("declares nothing — refusing a
// vacuous pass"), scripts/sync-manifest-proof.sh, and the Makefile's
// zero-target refusal in `sync`/`sync-status`.

func vacuousRepo(t *testing.T, withRule bool) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	if withRule {
		mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
			"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	}
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")
	return root
}

// Row 12, the sharpest member of #151: a tree holding a known violation and no
// rules reads as fully green from EVERY command — check 0/0 exit 0, and lint,
// the loud counterpart the whole issue leans on, reports 4/4 checks passed.
func TestCheckZeroRulesConfiguredExits2(t *testing.T) {
	root := vacuousRepo(t, false)
	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (no rules is a config error, not a pass)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "rules passed") {
		t.Errorf("a run with no rules must not report rules passed:\n%s", out)
	}
	for _, want := range []string{"no rules", ".formwork/rules"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q — the cause must be named and curable:\n%s", want, errOut)
		}
	}
}

// The same tree WITH a rule must still behave exactly as before: this is the
// control that stops the guard above from being satisfied by refusing
// everything.
func TestCheckWithRulesStillReportsFindings(t *testing.T) {
	root := vacuousRepo(t, true)
	code, out, _ := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "[no-widget] FAIL") {
		t.Fatalf("expected the violation to be found:\n%s", out)
	}
}

// Row 12 again, at the command the issue's whole argument rests on: "lint
// already treats the same condition as a defect". It does not, when the rule
// set itself is empty — every per-rule check has nothing to iterate and OKs.
//
// lint answers as a CHECK (exit 1, its normal verdict for problems found), not
// as an up-front refusal. The first cut of this fix refused, which preempted the
// checks that DO have something to say about a rule-less config; the test below
// is the pin for that and is the more important of the two.
func TestLintZeroRulesConfiguredIsAFailedCheck(t *testing.T) {
	root := vacuousRepo(t, false)
	code, out, errOut := runCLI(t, "lint", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[rules-present] FAIL") {
		t.Errorf("the empty rule set must be named as a failed check:\n%s", out)
	}
	if strings.Contains(out, "4/4 checks passed") {
		t.Errorf("lint reported an all-OK card over a config that enforces nothing:\n%s", out)
	}
}

// The regression this fix's own first cut introduced, and the reason lint
// reports rather than refuses. With lanes declared and no rules, lint has real
// itemised findings — lane-nonempty names each dead lane — and an up-front
// refusal threw them away in favour of one line claiming every check "would pass
// by having nothing to examine", which is false exactly here.
func TestLintZeroRulesStillRunsLaneChecks(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  ci:\n    all: true\n    ci: true\n  pre-commit:\n    tags: [go]\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")

	code, out, _ := runCLI(t, "lint", "-C", root)
	if code == 0 {
		t.Fatalf("exit = 0 over a rule-less config\n%s", out)
	}
	if !strings.Contains(out, "[rules-present] FAIL") {
		t.Errorf("missing the empty-rule-set check:\n%s", out)
	}
	// The load-bearing half: the lane diagnosis must survive, naming each lane.
	if !strings.Contains(out, "[lane-nonempty] FAIL") {
		t.Errorf("lane-nonempty was suppressed — the itemised diagnosis is the point:\n%s", out)
	}
	for _, lane := range []string{"ci", "pre-commit"} {
		if !strings.Contains(out, lane+": selects no rules") {
			t.Errorf("lane %q not named in the output:\n%s", lane, out)
		}
	}
}

// The cause-attribution ordering, which cli.go asserts in a comment ("configured
// is captured before any filter runs, so an empty .formwork/rules is never
// misreported as a lane or --skip-escapes problem") and nothing pinned. With
// zero rules AND a lane, both `configured == 0` and `selectedCount == 0` hold;
// only the first is the actionable cure, since adding the tag to a rule that
// does not exist is not advice.
func TestCheckZeroRulesWithLaneNamesTheRuleSet(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  ci:\n    all: true\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")

	code, _, errOut := runCLI(t, "check", "-C", root, "--lane", "ci")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, ".formwork/rules") {
		t.Errorf("cause must be the empty rule set, not the lane:\n%s", errOut)
	}
	if strings.Contains(errOut, "selects no rules") {
		t.Errorf("blamed the lane for an empty rule set:\n%s", errOut)
	}
}

// Same ordering question from the other side: --skip-escapes must not be blamed
// for a rule set that was empty before it ran.
func TestCheckZeroRulesWithSkipEscapesNamesTheRuleSet(t *testing.T) {
	root := vacuousRepo(t, false)
	code, _, errOut := runCLI(t, "check", "-C", root, "--skip-escapes")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, ".formwork/rules") || strings.Contains(errOut, "skip-escapes dropped") {
		t.Errorf("cause must be the empty rule set, not --skip-escapes:\n%s", errOut)
	}
}

// Row 12's third command. `formwork test` over zero rules reports "0/0 rules
// passed, 0 fixture(s) run" at exit 0 — the same green over an engine that
// evaluated nothing. Left unfixed it would make the class member only
// two-thirds repaired, which reads as fixed.
func TestTestZeroRulesConfiguredExits2(t *testing.T) {
	root := vacuousRepo(t, false)
	code, out, errOut := runCLI(t, "test", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "rules passed") {
		t.Errorf("test over zero rules must not report rules passed:\n%s", out)
	}
}

// The refusal must not take a BETTER diagnosis with it. Rule files gone (or
// mis-named .yml so the *.yaml glob misses them) while their fixture trees
// remain is a real shape, and the orphan-fixture error names the dead trees —
// far more actionable than "no rules are configured", and it was being
// swallowed by guarding ahead of fixturetest.Run.
func TestTestZeroRulesStillReportsOrphanFixtures(t *testing.T) {
	root := vacuousRepo(t, false)
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-widget", "fire-1", "a.go"), "package p\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-widget", "pass-1", "a.go"), "package p\n")

	code, _, errOut := runCLI(t, "test", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "no-widget") {
		t.Errorf("the orphan fixture tree must be named:\n%s", errOut)
	}
	if strings.Contains(errOut, "there are no fixtures to run") {
		t.Errorf("claimed there are no fixtures while two trees sit on disk:\n%s", errOut)
	}
}

// The cure must match the cause. Rule files PRESENT that declare no rules
// (`rules: []`, or a null `rules:` key — what a bad merge or a templating error
// leaves) is a different mistake from an absent .formwork/rules, and "add a rule
// file" is useless to someone looking at the rule file they just wrote.
func TestZeroRulesFromAnEmptyRuleFileNamesThatCause(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"), "rules: []\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")

	code, _, errOut := runCLI(t, "check", "-C", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "declared no rules") {
		t.Errorf("cause must be the empty declaration, not a missing file:\n%s", errOut)
	}
	if strings.Contains(errOut, "add a rule file") {
		t.Errorf("told the operator to add the rule file they are looking at:\n%s", errOut)
	}
}

// rules-for is the pre-hoc guidance primitive an external consumer reads, so an
// empty answer over a corpus that loaded no rules is a guidance fail-open: the
// caller is told the file is unconstrained. The genuine "(none)" — rules exist,
// none match this path — must survive untouched, and the second half of this
// test is what stops the guard from being satisfied by refusing everything.
func TestRulesForZeroRuleCorpusRefusesButKeepsGenuineNone(t *testing.T) {
	empty := vacuousRepo(t, false)
	code, out, errOut := runCLI(t, "rules-for", "-C", empty, "src/bad.go")
	if code != 2 {
		t.Fatalf("zero-rule corpus: exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "(none)") {
		t.Errorf("answered a wrong frame with a confident empty answer:\n%s", out)
	}

	governed := t.TempDir()
	mustWrite(t, filepath.Join(governed, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(governed, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: dart-only\n    type: forbidden-pattern\n    scope: {include: ['**/*.dart']}\n    params: {pattern: X}\n")
	mustWrite(t, filepath.Join(governed, "src", "a.go"), "package p\n")
	code, out, _ = runCLI(t, "rules-for", "-C", governed, "src/a.go")
	if code != 0 || !strings.Contains(out, "(none)") {
		t.Fatalf("the genuine (none) must be unchanged: exit = %d\n%s", code, out)
	}
}

// An explicitly-passed empty --range is a supplied flag, not an absent one.
// Silently falling back to a whole-tree scan makes a CI job fail on files the
// change never touched, with nothing saying the range was discarded.
func TestCheckEmptyRangeIsRefusedNotWidened(t *testing.T) {
	root := vacuousRepo(t, true)
	code, out, errOut := runCLI(t, "check", "-C", root, "--range", "")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stdout %q, stderr %q)", code, out, errOut)
	}
	if !strings.Contains(errOut, "--range") || !strings.Contains(errOut, "empty") {
		t.Errorf("stderr must name the empty --range:\n%s", errOut)
	}
}

// Flag validation before config content, for EVERY check-owned flag — not just
// the two that prompted the reorder. A mistyped -format on a rule-less repo was
// answered with advice about the rule set.
func TestCheckBadFormatNamesTheFormatNotTheRuleSet(t *testing.T) {
	root := vacuousRepo(t, false)
	code, _, errOut := runCLI(t, "check", "-C", root, "-format", "bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "bogus") {
		t.Errorf("stderr must name the bad format:\n%s", errOut)
	}
	if strings.Contains(errOut, "no rules are configured") {
		t.Errorf("a format typo was answered with advice about rules:\n%s", errOut)
	}
}

// Row 10: --lane names a lane that EXISTS but selects no rules. Unknown lane is
// already exit 2; a lane that resolves to nothing was exit 0 with not one rule
// line printed.
func TestCheckLaneSelectingZeroRulesExits2(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  sql-only:\n    tags: [sql]\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    tags: [go]\n    params: {pattern: WIDGET}\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"WIDGET\"\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "sql-only")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "sql-only") {
		t.Errorf("stderr must name the lane that selected nothing:\n%s", errOut)
	}
}

// Row 11: every rule is heavy, so --skip-escapes empties the rule set. A local
// hook then exits 0 having run nothing. The drop is legitimate; reporting the
// result as a pass is not.
func TestCheckSkipEscapesEmptyingRuleSetExits2(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: heavy-only\n    type: command\n    scope: {include: ['**/*.go']}\n"+
			"    params:\n      cmd: [\"true\"]\n      expect: {exit: 0}\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "package p\n")

	code, out, errOut := runCLI(t, "check", "-C", root, "--skip-escapes")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "skip-escapes") {
		t.Errorf("stderr must name --skip-escapes as the cause:\n%s", errOut)
	}
}

// The empty-`--range` guard's first cut went into runCheck alone, leaving
// runScope — same file, same flag, same `*rangeSpec != ""` fallback — to discard
// it silently. scope's fallback is the worse one: it classifies the STAGED set,
// empty in CI, and an empty changeset is `docs`, the weakest class, so a wrapper
// gating on scope skips every runtime check at exit 0.
func TestScopeEmptyRangeIsRefusedNotDowngraded(t *testing.T) {
	root := vacuousRepo(t, true)
	code, out, errOut := runCLI(t, "scope", "-C", root, "--range", "")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "class=docs") {
		t.Errorf("an empty --range was downgraded to the weakest class:\n%s", out)
	}
	if !strings.Contains(errOut, "--range") {
		t.Errorf("stderr must name the empty --range:\n%s", errOut)
	}
}

// rules-present cannot fail under --rule (scopeToRule refuses an id matching
// nothing, so a scoped config always carries a rule). Counting it would put a
// check that cannot fail into the denominator an operator reads as coverage —
// the same vacuity the check itself exists to report.
func TestLintScopedToRuleDoesNotCountRulesPresent(t *testing.T) {
	root := vacuousRepo(t, true)
	_, full, _ := runCLI(t, "lint", "-C", root)
	_, scoped, _ := runCLI(t, "lint", "-C", root, "--rule", "no-widget")
	if !strings.Contains(full, "[rules-present]") {
		t.Fatalf("unscoped lint should run the check:\n%s", full)
	}
	if strings.Contains(scoped, "[rules-present]") {
		t.Errorf("a check that cannot fail was counted under --rule:\n%s", scoped)
	}
}

// #157: `list` was the last member of the family still answering a config it
// never loaded confidently — nothing at exit 0, `[]` under -format json, over
// the same corpus check, test, lint and rules-for all refuse. The json format
// makes it a machine surface, so `[]` reads to a consumer as "this repository
// declares no guardrails" rather than "point me at the right checkout".
//
// The condition is the config having loaded NOTHING, not the enumeration
// happening to be empty — which is why both config-derived kinds refuse on the
// same zero-rule test, and why a legitimately lane-less corpus that HAS rules
// still prints an empty lane list at exit 0 (TestListLanesEmptyWithRulesStillZero).
func TestListRulesZeroRulesConfiguredExits2(t *testing.T) {
	root := vacuousRepo(t, false)
	code, out, errOut := runCLI(t, "list", "-C", root, "rules")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("list rules answered a config it never loaded:\n%s", out)
	}
	// The wording is rules-for's own, through the shared noRulesReason — the
	// point of the fix is that the family agrees, so a second phrasing would
	// only move the inconsistency into the message.
	for _, want := range []string{"no rules are configured", ".formwork/rules"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

func TestListRulesZeroRulesJSONExits2(t *testing.T) {
	root := vacuousRepo(t, false)
	code, out, errOut := runCLI(t, "list", "-C", root, "-format", "json", "rules")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "[]") {
		t.Errorf("a machine consumer was handed an empty enumeration of a config that never loaded:\n%s", out)
	}
}

func TestListLanesZeroRulesConfiguredExits2(t *testing.T) {
	root := t.TempDir()
	// Lanes declared, rules absent: the enumeration would be non-empty, so an
	// implementation guarding "this list came out empty" instead of "the config
	// loaded nothing" passes here and still answers the wrong frame.
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
		"version: 1\nlanes:\n  ci:\n    all: true\n    ci: true\n")
	code, out, errOut := runCLI(t, "list", "-C", root, "lanes")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "ci") {
		t.Errorf("lanes were enumerated over a corpus with no rules to run in them:\n%s", out)
	}
	if !strings.Contains(errOut, "no rules are configured") {
		t.Errorf("stderr missing the shared reason:\n%s", errOut)
	}
	// The reason must not argue a counterfactual this very corpus contradicts.
	// A lane IS declared here: add one rule file to this same config and
	// `list lanes` prints `ci`, so "an empty list would read as this
	// repository declares no lanes" is false exactly where it is printed. The
	// operator reading it is looking at their own `lanes:` block.
	if strings.Contains(errOut, "declares no lanes") {
		t.Errorf("the refusal claims the list would have been empty, but this corpus declares a lane:\n%s", errOut)
	}
}

// The control that stops the guard above from being satisfied by refusing
// everything: a corpus WITH rules and NO lanes is a legitimate config, and its
// empty lane list is a truthful enumeration, not a wrong frame.
func TestListLanesEmptyWithRulesStillZero(t *testing.T) {
	root := vacuousRepo(t, true)
	code, out, errOut := runCLI(t, "list", "-C", root, "lanes")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("no lanes are declared, so nothing should be listed:\n%s", out)
	}
}

// The altitude control. `types` and `preprocessors` come from the registries and
// never load config at all, so they must keep answering in a corpus that
// declares no rules — a guard placed above runList's switch, or anywhere that
// makes the registry kinds load config, breaks exactly this.
func TestListRegistryKindsUnaffectedByAZeroRuleCorpus(t *testing.T) {
	root := vacuousRepo(t, false)
	for _, kind := range []string{"types", "preprocessors"} {
		code, out, errOut := runCLI(t, "list", "-C", root, kind)
		if code != 0 {
			t.Fatalf("list %s: exit = %d, want 0\nstderr:\n%s", kind, code, errOut)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("list %s enumerated nothing:\n%s", kind, out)
		}
	}
}
