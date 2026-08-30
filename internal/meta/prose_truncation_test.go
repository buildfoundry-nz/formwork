// prose_truncation_test.go — #59.
//
// A plain (unquoted) YAML scalar ends at ` #`: the rest is a comment. So
//
//	cure: converted call sites consume the accessor (audit-1 #14)
//
// reaches the engine as "converted call sites consume the accessor (audit-1"
// and the rule reports that cure, truncated, for its whole life. No error, no
// warning — this was found downstream only when someone read the output
// carefully, and issue-number references in cures are exactly the house style
// that triggers it.
//
// The check cannot decide intent, and that is the point: `cure: do it  # note`
// is legitimate YAML with a deliberate trailing comment, and by the time the
// decoder hands over a string both cases look identical. So it reports rather
// than guesses, and asks the author to quote — which makes the intent explicit
// either way.
package meta_test

import (
	"fmt"
	"strings"
	"testing"
)

const truncRuleTemplate = "rules:\n" +
	"  - id: no-ghost\n" +
	"    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    params: {pattern: 'Ghost'}\n" +
	"    cure: %s\n"

// A base that lints CLEAN, so the only thing a verdict can be about is the
// cure line under test. Without the fixtures and the in-scope source file,
// fixture-coverage and empty-scope fail and every case below "passes" for the
// wrong reason.
func truncRepo(cure string) map[string]string {
	return map[string]string{
		".formwork/formwork.yaml":                 "version: 1\n",
		".formwork/rules/r.yaml":                  fmt.Sprintf(truncRuleTemplate, cure),
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"src.go": "package p\n",
	}
}

func TestLintReportsATruncatedPlainScalar(t *testing.T) {
	failed, out := lint(t, truncRepo("converted call sites consume the accessor (audit-1 #14)"))
	if failed == 0 {
		t.Fatalf("a plain scalar whose tail YAML ate must be reported:\n%s", out)
	}
	if !strings.Contains(out, "cure") {
		t.Fatalf("the report must name the field, got:\n%s", out)
	}
	// The value the engine actually received, so the reader can see the loss
	// rather than take it on trust.
	if !strings.Contains(out, "(audit-1") {
		t.Fatalf("the report must show the truncated value, got:\n%s", out)
	}
}

// Quoting is the fix, and it must clear the report — otherwise the check has no
// cure of its own and an author cannot make it stop.
func TestLintPassesTheSameTextQuoted(t *testing.T) {
	failed, out := lint(t, truncRepo("\"converted call sites consume the accessor (audit-1 #14)\""))
	if failed != 0 {
		t.Fatalf("a quoted scalar keeps its whole value and must not be reported:\n%s", out)
	}
}

// `#` is only a comment when whitespace precedes it. A plain scalar carrying a
// glued `#` keeps it, so reporting one would be a false positive — and this is
// the narrowing that stops the check firing on ordinary text.
func TestLintDoesNotReportAGluedHash(t *testing.T) {
	failed, out := lint(t, truncRepo("see issue#14 for the accessor rewrite"))
	if failed != 0 {
		t.Fatalf("a glued # is not a comment and must not be reported:\n%s", out)
	}
}

// The second narrowing. A single-token value followed by a comment is the
// ordinary, intentional YAML shape — `cap: 40  # taste, not correctness` in the
// quickstart corpus is exactly it. Reporting those would make the check noise
// an author learns to skim, which is how a real truncation gets missed.
func TestLintDoesNotReportASingleTokenValueWithAComment(t *testing.T) {
	files := truncRepo("\"quoted so the cure line itself is clean\"")
	files[".formwork/rules/r.yaml"] = strings.Replace(files[".formwork/rules/r.yaml"],
		"    params: {pattern: 'Ghost'}\n",
		"    params:\n      pattern: Ghost # the removed feature's name\n", 1)
	failed, out := lint(t, files)
	if failed != 0 {
		t.Fatalf("a single-token value plus a trailing comment must not be reported:\n%s", out)
	}
}
