// lintpolicy_test.go — the corpus-selectable check set (#89). Separate from
// lint_test.go, which the 750-line vendor cap bounds; same package.
package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/meta"
)

// A repo whose empty-scope and exemption-hygiene checks both have real
// problems: a rule scoped to a directory that does not exist, carrying a dead
// exclude with no justification comment. This is the shape #89 reports for the
// palletra-port-* corpora — thin slices of a much larger tree, where those two
// verdicts are properties of the fixture material rather than defects.
const thinSliceRules = "rules:\n" +
	"  - id: absent-corpus\n" +
	"    type: forbidden-pattern\n" +
	"    scope:\n" +
	"      include: ['generated/**']\n" +
	"      exclude: ['**/*.pb.go']\n" +
	"    params: {pattern: banana}\n"

var thinSliceRepo = map[string]string{
	".formwork/formwork.yaml":                      "version: 1\n",
	".formwork/rules/r.yaml":                       thinSliceRules,
	".formwork/fixtures/absent-corpus/fire-1/f.go": "banana want: absent-corpus\n",
	".formwork/fixtures/absent-corpus/pass-1/f.go": "clean\n",
	"notes.txt": "the whole corpus\n",
}

// lintErr is lint() for the refusal cases: it returns the error instead of
// failing the test on it.
func lintErr(t *testing.T, files map[string]string) (int, string, error) {
	t.Helper()
	return lintRootErr(t, writeRepo(t, files))
}

func withLintPolicy(files map[string]string, policy string) map[string]string {
	out := make(map[string]string, len(files)+1)
	for k, v := range files {
		out[k] = v
	}
	out[".formwork/lint.yaml"] = policy
	return out
}

// The corpora are unmodified fixture material, so the mechanism is a declared,
// justified skip list — visible in the output, never a silent Makefile flag.
func TestLintSkipsADeclaredCheckAndDisclosesItsReason(t *testing.T) {
	failed, out := lint(t, withLintPolicy(thinSliceRepo,
		"version: 1\n"+
			"skip:\n"+
			"  - check: empty-scope\n"+
			"    reason: thin slice of a much larger tree\n"+
			"  - check: exemption-hygiene\n"+
			"    reason: excludes name generated trees this slice omits\n"))
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[empty-scope] SKIPPED — thin slice of a much larger tree",
		"[exemption-hygiene] SKIPPED — excludes name generated trees this slice omits",
		"formwork lint: 3/3 checks passed (2 skipped: empty-scope, exemption-hygiene — see .formwork/lint.yaml)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// A skipped check must not also claim a verdict.
	if strings.Contains(out, "[empty-scope] OK") || strings.Contains(out, "[empty-scope] FAIL") {
		t.Fatalf("a skipped check reported a verdict as well:\n%s", out)
	}
}

// Without the file nothing changes: every check runs and the summary reads
// exactly as it did before the mechanism existed.
func TestLintWithoutAPolicyFileRunsEveryCheck(t *testing.T) {
	failed, out := lint(t, thinSliceRepo)
	if failed != 2 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "SKIPPED") || strings.Contains(out, "skipped:") {
		t.Fatalf("no policy file must mean no skips:\n%s", out)
	}
	if !strings.Contains(out, "formwork lint: 3/5 checks passed\n") {
		t.Fatalf("summary must be unchanged:\n%s", out)
	}
}

// The skip list is an escape hatch, and this repo's escape hatches carry their
// justification. An unreasoned skip is a config error, not a quiet skip.
func TestLintRefusesASkipWithNoReason(t *testing.T) {
	_, out, err := lintErr(t, withLintPolicy(thinSliceRepo,
		"version: 1\nskip:\n  - check: empty-scope\n    reason: \"  \"\n"))
	if err == nil {
		t.Fatalf("an unjustified skip must be exit 2:\n%s", out)
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Fatalf("the refusal must name what is missing: %v", err)
	}
}

// Fail-closed on a name this binary never runs: a typo, or a check that was
// renamed or deleted, would otherwise leave a skip entry silently protecting
// nothing — the dead-exclude defect (#55) reproduced one level up.
func TestLintRefusesASkipForACheckItNeverRan(t *testing.T) {
	_, out, err := lintErr(t, withLintPolicy(thinSliceRepo,
		"version: 1\nskip:\n  - check: emty-scope\n    reason: typo\n"))
	if err == nil {
		t.Fatalf("an unknown check name must be exit 2:\n%s", out)
	}
	if !strings.Contains(err.Error(), "emty-scope") {
		t.Fatalf("the refusal must name the entry: %v", err)
	}
}

// A conditional check nothing in this corpus reaches (lane-reachability with no
// lanes configured) is the same dead entry as a typo, and refused the same way.
func TestLintRefusesASkipForACheckThisCorpusNeverReaches(t *testing.T) {
	_, out, err := lintErr(t, withLintPolicy(thinSliceRepo,
		"version: 1\nskip:\n  - check: lane-reachability\n    reason: no lanes here\n"))
	if err == nil {
		t.Fatalf("a skip for a check this corpus never reaches must be exit 2:\n%s", out)
	}
	if !strings.Contains(err.Error(), "lane-reachability") {
		t.Fatalf("the refusal must name the entry: %v", err)
	}
}

func TestLintRefusesAMalformedPolicyFile(t *testing.T) {
	for _, tc := range []struct{ name, policy, want string }{
		{"unknown field", "version: 1\nskips:\n  - check: empty-scope\n", "skips"},
		{"unknown version", "version: 2\nskip: []\n", "version"},
		{"duplicate entry", "version: 1\nskip:\n  - check: empty-scope\n    reason: a\n  - check: empty-scope\n    reason: b\n", "empty-scope"},
		{"no check name", "version: 1\nskip:\n  - reason: a\n", "check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := lintErr(t, withLintPolicy(thinSliceRepo, tc.policy))
			if err == nil {
				t.Fatalf("expected exit 2:\n%s", out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal must mention %q: %v", tc.want, err)
			}
		})
	}
}

// Skipping must stop the check's WORK, not merely its output line. Proven by a
// fixture directory lint cannot read: computing fixture-coverage over it is an
// engine error (exit 2), so a run that comes back clean is one where the check
// genuinely did not run.
func TestLintSkipStopsTheCheckFromRunningAtAll(t *testing.T) {
	skipUnlessChmodEnforced(t)
	files := withLintPolicy(thinSliceRepo,
		"version: 1\n"+
			"skip:\n"+
			"  - check: fixture-coverage\n    reason: unreadable on purpose\n"+
			"  - check: empty-scope\n    reason: thin slice\n"+
			"  - check: exemption-hygiene\n    reason: thin slice\n")
	root := writeRepo(t, files)
	dir := filepath.Join(root, ".formwork", "fixtures", "absent-corpus")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	})

	failed, out, err := lintRootErr(t, root)
	if err != nil {
		t.Fatalf("a skipped check must not do its work: %v\n%s", err, out)
	}
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[fixture-coverage] SKIPPED") {
		t.Fatalf("the skip must still be disclosed:\n%s", out)
	}
}

// lintScopedErr is lintErr for a `--rule` run: the flag the CLI sets after
// narrowing cfg.Rules to one rule. Written as the flag rather than by calling
// the CLI because meta.Lint is where the narrowing is observable.
func lintScopedErr(t *testing.T, files map[string]string) (int, string, error) {
	t.Helper()
	root := writeRepo(t, files)
	var sb strings.Builder
	failed, err := meta.Lint(mustLoad(t, root), root, &sb, false, true)
	return failed, sb.String(), err
}

// rules-present is the guard against a config that enforces nothing being
// reported as an all-OK card (#151). A corpus that could switch it off could
// buy back exactly the vacuity it exists to report, so the skip list refuses
// the entry at load — before any verdict is printed.
func TestLintRefusesASkipForRulesPresent(t *testing.T) {
	_, out, err := lintErr(t, withLintPolicy(thinSliceRepo,
		"version: 1\nskip:\n  - check: rules-present\n    reason: we would rather not know\n"))
	if err == nil {
		t.Fatalf("skipping rules-present must be exit 2:\n%s", out)
	}
	if !strings.Contains(err.Error(), "rules-present") {
		t.Fatalf("the refusal must name the entry: %v", err)
	}
	// It must refuse AS UNSKIPPABLE, not as a dead entry the run never reached.
	// The dead-entry message tells the author to arm the check or delete the
	// line, which for this one is wrong advice in both directions.
	if !strings.Contains(err.Error(), "cannot be skipped") {
		t.Fatalf("the refusal must say the check is not skippable, not that the run missed it: %v", err)
	}
	if strings.Contains(out, "SKIPPED") {
		t.Fatalf("the refusal must precede any verdict or disclosure:\n%s", out)
	}
}

// `--rule` narrows cfg.Rules, so a conditional check can be unarmed by the
// NARROWING rather than by the corpus — prefilter-load-bearing here. The
// dead-entry refusal cannot tell those apart, so it made a valid config plus a
// valid rule id exit 2, inverting the exit-code contract (2 is for a broken
// engine or config, and nothing here is broken). It holds for whole-corpus runs,
// which is where dead config actually rots.
func TestLintDoesNotRefuseADeadSkipEntryUnderRuleScoping(t *testing.T) {
	failed, out, err := lintScopedErr(t, withLintPolicy(thinSliceRepo,
		"version: 1\nskip:\n  - check: prefilter-load-bearing\n    reason: no prefilters in this slice\n"))
	if err != nil {
		t.Fatalf("a scoped run must not refuse an unreached skip: %v\n%s", err, out)
	}
	if failed != 2 {
		t.Fatalf("failed=%d — the checks that did run must still report\n%s", failed, out)
	}
}

// The floor above holds for a whole-corpus run. Under --rule it must REPORT
// rather than refuse: rules-present is whole-corpus, so a corpus declaring the
// per-rule checks inapplicable has an empty board for any single rule through
// no fault of its config — and `lint --rule <valid-id>` on
// examples/palletra-port-full was exit 2 for exactly that. A valid config plus
// a valid rule id is not an engine error.
//
// What stops that being the vacuity the floor exists to catch is the wording,
// so that is what is asserted: it says no check ran, and it never prints the
// pass card.
func TestScopedLintReportsAnEmptyBoardRatherThanRefusing(t *testing.T) {
	files := withLintPolicy(thinSliceRepo,
		"version: 1\n"+
			"skip:\n"+
			"  - check: prose-not-truncated\n    reason: thin slice\n"+
			"  - check: fixture-coverage\n    reason: thin slice\n"+
			"  - check: empty-scope\n    reason: thin slice\n"+
			"  - check: exemption-hygiene\n    reason: thin slice\n")

	failed, out, err := lintScopedErr(t, files)
	if err != nil {
		t.Fatalf("a scoped run over an empty board must not refuse: %v\n%s", err, out)
	}
	if failed != 0 {
		t.Fatalf("failed=%d — an empty board has nothing to fail\n%s", failed, out)
	}
	if !strings.Contains(out, "no lint check ran") {
		t.Fatalf("a scoped run over an empty board must say so:\n%s", out)
	}
	if strings.Contains(out, "checks passed") {
		t.Fatalf("an empty board must never be reported as a pass:\n%s", out)
	}
}
