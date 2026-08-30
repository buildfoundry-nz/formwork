package report_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/report"
)

// #57: every renderer reported how MANY findings were suppressed and none
// reported WHICH, so the exemption surface could not be audited from check
// output at any format. JSON and GitHub grew their enumerations in #91; Human
// is the member of that class still computing `len(findings) - len(live)` and
// printing the integer alone.
//
// Two properties, not one. The visible half is that each suppressed finding is
// named — rule, path, line, message, channel. The structural half is that the
// COUNT derives from the enumeration, so the two cannot disagree: today they
// cannot disagree only because one of them does not exist.

func TestHumanEnumeratesSuppressedFindings(t *testing.T) {
	rls := []*config.Rule{
		mustRule(t, "a-clean", finding.SeverityError, ""),
		mustRule(t, "b-marked", finding.SeverityError, ""),
		mustRule(t, "c-allowlisted", finding.SeverityError, ""),
	}
	findings := []finding.Finding{
		{RuleID: "b-marked", Severity: finding.SeverityError, Path: "x.go", Line: 7, Message: "bad",
			Suppressed: true, SuppressedBy: "marker"},
		{RuleID: "c-allowlisted", Severity: finding.SeverityError, Path: "y.go", Message: "waived",
			Suppressed: true, SuppressedBy: "allowlist:allow.txt:3"},
	}
	var sb strings.Builder
	report.Human(&sb, rls, findings, report.ScanSummary{})
	want := `[a-clean] OK
[b-marked] OK
[c-allowlisted] OK
scan: 0 file(s) scanned
suppressed (exempted, not failures):
  [b-marked] x.go:7: bad (marker)
  [c-allowlisted] y.go: waived (allowlist:allow.txt:3)
formwork: 3/3 rules passed, 0 finding(s), 2 suppressed
`
	if got := sb.String(); got != want {
		t.Fatalf("output mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A scope-level suppressed finding has no path; it must still be named, not
// dropped from the enumeration for lacking a location.
func TestHumanEnumeratesPathlessSuppressedFinding(t *testing.T) {
	rls := []*config.Rule{mustRule(t, "a-scope", finding.SeverityError, "")}
	findings := []finding.Finding{
		{RuleID: "a-scope", Severity: finding.SeverityError, Message: "scope-level bad",
			Suppressed: true, SuppressedBy: "allowlist:allow.txt:1"},
	}
	var sb strings.Builder
	report.Human(&sb, rls, findings, report.ScanSummary{})
	if !strings.Contains(sb.String(), "  [a-scope] scope-level bad (allowlist:allow.txt:1)\n") {
		t.Fatalf("a path-less suppressed finding must still be named:\n%s", sb.String())
	}
}

var humanSuppressedTail = regexp.MustCompile(`, (\d+) suppressed\n$`)

// The structural half of #57: the summary's count is len(the enumeration), not
// arithmetic asserted beside it. Read back off the rendered text — the number
// in the summary line must equal the number of enumerated lines, whatever the
// renderer chose to enumerate. A renderer that drops an entry from the list
// while the count keeps counting the slice fails here even though every
// individual line it did print is correct.
//
// Mutation-checked, and worth stating precisely: swapping the summary back to
// len(findings)-len(live) ON ITS OWN leaves this green, because against a
// complete list the two expressions agree. What this test catches is the
// DIVERGENCE only an independent count can produce — restore that arithmetic
// AND drop one entry from suppressedLines and it goes red. That pairing is the
// regression shape, and it is unreachable while the count is len(the list).
func TestHumanSuppressedCountDerivesFromTheEnumeration(t *testing.T) {
	var rls []*config.Rule
	var findings []finding.Finding
	// A mix of the shapes a renderer might be tempted to skip: line 0, empty
	// path, and an ordinary located finding.
	for i, f := range []finding.Finding{
		{RuleID: "r0", Path: "a.go", Line: 1, Message: "m0", SuppressedBy: "marker"},
		{RuleID: "r1", Path: "b.go", Message: "m1", SuppressedBy: "marker"},
		{RuleID: "r2", Message: "m2", SuppressedBy: "allowlist:allow.txt:9"},
	} {
		rls = append(rls, mustRule(t, "r"+strconv.Itoa(i), finding.SeverityError, ""))
		f.Severity = finding.SeverityError
		f.Suppressed = true
		findings = append(findings, f)
	}
	var sb strings.Builder
	report.Human(&sb, rls, findings, report.ScanSummary{})
	out := sb.String()

	m := humanSuppressedTail.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("summary line carries no suppressed count:\n%s", out)
	}
	claimed, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	enumerated := 0
	inSection := false
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "suppressed ("):
			inSection = true
		case inSection && strings.HasPrefix(l, "  "):
			enumerated++
		case inSection:
			inSection = false
		}
	}
	if enumerated != claimed {
		t.Fatalf("summary claims %d suppressed but %d were enumerated — the count must be len(the list):\n%s",
			claimed, enumerated, out)
	}
	if claimed != len(findings) {
		t.Fatalf("claimed %d, want all %d suppressed findings accounted for:\n%s", claimed, len(findings), out)
	}
}

// Zero suppressions renders no section at all — an empty heading would be
// noise on the run that is by far the commonest.
func TestHumanOmitsSuppressedSectionWhenNone(t *testing.T) {
	rls := []*config.Rule{mustRule(t, "a-clean", finding.SeverityError, "")}
	findings := []finding.Finding{
		{RuleID: "a-clean", Severity: finding.SeverityError, Path: "x.go", Line: 1, Message: "live"},
	}
	var sb strings.Builder
	report.Human(&sb, rls, findings, report.ScanSummary{})
	if strings.Contains(sb.String(), "suppressed") {
		t.Fatalf("no suppressions must render no suppressed section:\n%s", sb.String())
	}
}
