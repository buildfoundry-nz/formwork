package report_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/report"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type nopChecker struct{}

func (nopChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func mustRule(t *testing.T, id string, sev finding.Severity, cure string) *config.Rule {
	t.Helper()
	r, err := config.New(id, "fake", sev, cure, []string{"**"}, nil, nil, nopChecker{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHumanRendersOKFailWarnAndSummary(t *testing.T) {
	rls := []*config.Rule{
		mustRule(t, "a-clean", finding.SeverityError, ""),
		mustRule(t, "b-broken", finding.SeverityError, "Fix the thing."),
		mustRule(t, "c-warned", finding.SeverityWarn, ""),
	}
	findings := []finding.Finding{
		{RuleID: "b-broken", Severity: finding.SeverityError, Path: "src/x.go", Line: 3, Message: "bad"},
		{RuleID: "b-broken", Severity: finding.SeverityError, Path: "src/y.go", Line: 0, Message: "file-level bad"},
		{RuleID: "c-warned", Severity: finding.SeverityWarn, Path: "", Line: 0, Message: "scope-level warn"},
	}
	var sb strings.Builder
	report.Human(&sb, rls, findings, report.ScanSummary{})
	got := sb.String()
	want := `[a-clean] OK
[b-broken] FAIL — 2 finding(s)
  src/x.go:3: bad
  src/y.go: file-level bad
  Cure: Fix the thing.
[c-warned] WARN — 1 finding(s)
  scope-level warn
scan: 0 file(s) scanned
formwork: 2/3 rules passed, 3 finding(s)
`
	if got != want {
		t.Fatalf("output mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestHumanAppendsSuppressedCountToSummary(t *testing.T) {
	// G1b: a suppressed finding never renders per-rule and never counts
	// toward the finding tally, but its existence must not be wholly
	// invisible from `check` output — the summary line's trailing
	// `, N suppressed` is Human's job now that it receives the full
	// findings slice (cli.go no longer pre-filters with
	// finding.Unsuppressed before calling Human).
	//
	// #57 added the enumeration above that line; this test keeps pinning the
	// SUMMARY, and suppressed_test.go owns the section's own contract.
	rls := []*config.Rule{
		mustRule(t, "a-clean", finding.SeverityError, ""),
		mustRule(t, "b-suppressed", finding.SeverityError, ""),
	}
	findings := []finding.Finding{
		{RuleID: "b-suppressed", Severity: finding.SeverityError, Path: "x.go", Line: 1, Message: "bad",
			Suppressed: true, SuppressedBy: "marker"},
	}
	var sb strings.Builder
	report.Human(&sb, rls, findings, report.ScanSummary{})
	got := sb.String()
	want := `[a-clean] OK
[b-suppressed] OK
scan: 0 file(s) scanned
suppressed (exempted, not failures):
  [b-suppressed] x.go:1: bad (marker)
formwork: 2/2 rules passed, 0 finding(s), 1 suppressed
`
	if got != want {
		t.Fatalf("output mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestHumanSummaryOmitsSuppressedTailWhenZero(t *testing.T) {
	// Existing summary text without suppressions must stay byte-identical
	// (goldens/tests pin it) — this pins the no-suppression case explicitly
	// alongside TestHumanRendersOKFailWarnAndSummary.
	rls := []*config.Rule{mustRule(t, "a-clean", finding.SeverityError, "")}
	var sb strings.Builder
	report.Human(&sb, rls, nil, report.ScanSummary{})
	want := "[a-clean] OK\nscan: 0 file(s) scanned\nformwork: 1/1 rules passed, 0 finding(s)\n"
	if sb.String() != want {
		t.Fatalf("got %q, want %q", sb.String(), want)
	}
}

// A mid-port corpus legitimately has hundreds of rules whose scope matches
// nothing — 572 of the 707 in examples/palletra-port-full — and printing all of
// them puts a wall of text between the findings and the verdict line, which
// buries both. The list is capped, and the cap is DISCLOSED with the exact
// count dropped and where to get the rest: a silent truncation would read as
// "that was all of them", which is the same class of quiet answer this whole
// change exists to remove. JSON is uncapped — a machine consumer has no
// readability problem and asking it to paginate would be worse.
func TestHumanCapsTheVacuousRuleListAndSaysSo(t *testing.T) {
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = string(rune('a'+i/10)) + string(rune('0'+i%10))
	}
	var sb strings.Builder
	report.Human(&sb, nil, nil, report.ScanSummary{FilesScanned: 3, RulesMatchingNoFiles: ids})
	got := sb.String()
	if strings.Count(got, ": scope matched no files") != 10 {
		t.Errorf("want 10 listed, got:\n%s", got)
	}
	if !strings.Contains(got, "… and 15 more rule(s) matched no files") {
		t.Errorf("the dropped count must be stated exactly:\n%s", got)
	}
	if !strings.Contains(got, "formwork lint") {
		t.Errorf("the capped list must say where the rest is:\n%s", got)
	}
}

// The cap must not fire below the threshold: a repo with a handful of vacuous
// rules gets all of them, and no overflow line at all.
func TestHumanDoesNotCapAtOrBelowTheLimit(t *testing.T) {
	ids := []string{"a", "b", "c"}
	var sb strings.Builder
	report.Human(&sb, nil, nil, report.ScanSummary{RulesMatchingNoFiles: ids})
	if got := sb.String(); strings.Contains(got, "more rule(s)") {
		t.Errorf("no overflow line below the cap:\n%s", got)
	}
}
