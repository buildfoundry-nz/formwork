package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/report"
)

func rule(id string) *config.Rule { return &config.Rule{ID: id} }

func sampleFindings() []finding.Finding {
	return []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "x.go", Line: 3, Message: "bad thing"},
		{RuleID: "w", Severity: finding.SeverityWarn, Message: "scope msg"},
		{RuleID: "b", Severity: finding.SeverityError, Path: "y.go", Message: "hidden", Suppressed: true, SuppressedBy: "marker"},
	}
}

func TestJSONReport(t *testing.T) {
	var buf bytes.Buffer
	report.JSON(&buf, []*config.Rule{rule("a"), rule("b")}, sampleFindings(), report.ScanSummary{})

	var rep struct {
		Findings []struct {
			Rule, Severity, Path, Message string
			Line                          int
		} `json:"findings"`
		Summary struct {
			RulesTotal  int `json:"rules_total"`
			RulesPassed int `json:"rules_passed"`
			Findings    int `json:"findings"`
			Suppressed  int `json:"suppressed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	// live findings: a(error) + w(warn) = 2; b is suppressed.
	if rep.Summary.Findings != 2 || rep.Summary.Suppressed != 1 {
		t.Fatalf("summary = %+v", rep.Summary)
	}
	// rules a,b: a has an error finding → fails; b's only finding is suppressed → passes.
	if rep.Summary.RulesTotal != 2 || rep.Summary.RulesPassed != 1 {
		t.Fatalf("rules summary = %+v", rep.Summary)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings array = %d", len(rep.Findings))
	}
	if rep.Findings[0].Rule != "a" || rep.Findings[0].Line != 3 {
		t.Fatalf("first finding = %+v", rep.Findings[0])
	}
}

// TestJSONNamesSuppressedFindings pins #91's JSON half: the suppressed
// findings are enumerated (rule/path/line/message/suppressed_by, engine order
// preserved) and the summary count is DERIVED from that list — the two cannot
// drift because one is the length of the other.
func TestJSONNamesSuppressedFindings(t *testing.T) {
	fs := []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "x.go", Line: 3, Message: "bad thing"},
		{RuleID: "b", Severity: finding.SeverityError, Path: "y.go", Line: 7, Message: "hidden", Suppressed: true, SuppressedBy: "marker"},
		{RuleID: "c", Severity: finding.SeverityError, Path: "z.go", Line: 9, Message: "waived", Suppressed: true, SuppressedBy: "allowlist:allow.txt:3"},
	}
	var buf bytes.Buffer
	report.JSON(&buf, []*config.Rule{rule("a"), rule("b"), rule("c")}, fs, report.ScanSummary{})

	var rep struct {
		Suppressed []struct {
			Rule, Severity, Path, Message string
			Line                          int
			SuppressedBy                  string `json:"suppressed_by"`
		} `json:"suppressed"`
		Summary struct {
			Suppressed int `json:"suppressed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(rep.Suppressed) != 2 {
		t.Fatalf("suppressed array = %+v, want the 2 suppressed findings enumerated", rep.Suppressed)
	}
	if rep.Suppressed[0].Rule != "b" || rep.Suppressed[0].Line != 7 || rep.Suppressed[0].SuppressedBy != "marker" {
		t.Fatalf("first suppressed entry = %+v", rep.Suppressed[0])
	}
	if rep.Suppressed[1].SuppressedBy != "allowlist:allow.txt:3" {
		t.Fatalf("second suppressed entry = %+v", rep.Suppressed[1])
	}
	if rep.Summary.Suppressed != len(rep.Suppressed) {
		t.Fatalf("summary.suppressed = %d, want len(suppressed) = %d — the count must derive from the list",
			rep.Summary.Suppressed, len(rep.Suppressed))
	}
}

// TestJSONSuppressedEmptyIsArrayNotNull asserts on the raw bytes: a machine
// consumer iterating .suppressed must get [] on a clean run, never null.
func TestJSONSuppressedEmptyIsArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	report.JSON(&buf, []*config.Rule{rule("a")}, []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "x.go", Line: 3, Message: "bad thing"},
	}, report.ScanSummary{})
	if !strings.Contains(buf.String(), `"suppressed": []`) {
		t.Fatalf("suppressed must encode as [] when empty, got:\n%s", buf.String())
	}
}

// TestJSONCarriesCure pins #107's JSON half: a live finding whose rule
// declares cure: carries it as an additive "cure" field, and a finding whose
// rule has none omits the key entirely (omitempty — zero diff for cure-less
// rules, the JSON shape is a nascent contract).
func TestJSONCarriesCure(t *testing.T) {
	rls := []*config.Rule{
		{ID: "a", Cure: "run make fmt"},
		rule("w"),
		// b's finding is suppressed; its cure must NOT surface on the
		// suppressed entry — an exempted finding is not asking to be
		// remediated. Without a cure here, a mutant adding Cure to
		// jsonSuppressed would stay green.
		{ID: "b", Cure: "should never surface"},
	}
	var buf bytes.Buffer
	report.JSON(&buf, rls, sampleFindings(), report.ScanSummary{})

	var rep struct {
		Findings []struct {
			Rule string
			Cure string `json:"cure"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if rep.Findings[0].Rule != "a" || rep.Findings[0].Cure != "run make fmt" {
		t.Fatalf("finding for cured rule = %+v, want cure carried", rep.Findings[0])
	}
	// The cure-less rule's finding must omit the key, not emit "" — and the
	// suppressed entry for cured rule b must not carry one either. Assert on
	// the raw bytes: exactly one "cure" key in the whole document.
	if got := strings.Count(buf.String(), `"cure"`); got != 1 {
		t.Fatalf(`"cure" key count = %d, want exactly 1 (omitempty for cure-less rules; none on suppressed entries):%s`, got, buf.String())
	}
	if strings.Contains(buf.String(), "should never surface") {
		t.Fatalf("suppressed entry leaked its rule's cure:\n%s", buf.String())
	}
}

// TestGitHubCarriesCure pins #107's PR-lane half: the cure is appended to the
// live annotation's message, routed through the same workflow-command data
// escaping as the message itself — all three data rules (% → %25, \r → %0D,
// \n → %0A); the multiline cure here would otherwise truncate the annotation
// at the first line break.
func TestGitHubCarriesCure(t *testing.T) {
	rls := []*config.Rule{
		{ID: "a", Cure: "run make fmt\r\nthen 100% commit"},
		rule("w"),
		// b's finding is suppressed; its cure must NOT surface on the
		// ::notice line — an exempted finding is not asking to be
		// remediated. Without a cure here, a mutant appending cure to
		// notices would stay green.
		{ID: "b", Cure: "should never surface"},
	}
	var buf bytes.Buffer
	report.GitHub(&buf, rls, sampleFindings(), report.ScanSummary{})
	out := buf.String()
	want := "::error file=x.go,line=3::bad thing (a)%0ACure: run make fmt%0D%0Athen 100%25 commit\n"
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
	// The cure-less rule's annotation is byte-identical to today's form.
	if !strings.Contains(out, "::warning::scope msg (w)\n") {
		t.Fatalf("cure-less annotation changed:\n%s", out)
	}
	// The suppressed notice for cured rule b stays exactly cure-free.
	if !strings.Contains(out, "::notice file=y.go::suppressed: hidden (b; marker)\n") {
		t.Fatalf("suppressed notice changed:\n%s", out)
	}
	if strings.Contains(out, "should never surface") {
		t.Fatalf("suppressed notice leaked its rule's cure:\n%s", out)
	}
}

// Mirrors of the renderer's github line-budget arithmetic (rule id "a",
// finding at x.go:3), so boundary tests land exactly where intended.
const (
	tpPrefix = "::error file=x.go,line=3::"
	tpJoiner = "%0ACure: "
	tpMarker = "… (cure truncated; run formwork explain a)"
)

// tpRawMsgFor returns the raw message length that leaves exactly avail bytes
// of line budget for the escaped cure fragment (message data is raw + " (a)").
func tpRawMsgFor(avail int) int {
	return 4096 - len(tpPrefix) - len(" (a)") - len(tpJoiner) - len(tpMarker) - avail
}

func ghCureLine(t *testing.T, rawMsg, cure string) string {
	t.Helper()
	fs := []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "x.go", Line: 3, Message: rawMsg},
	}
	var buf bytes.Buffer
	report.GitHub(&buf, []*config.Rule{{ID: "a", Cure: cure}}, fs, report.ScanSummary{})
	return ghAnnotationLine(t, buf.String())
}

// ghAnnotationLine returns the single FINDING annotation from a GitHub render,
// separating it from the scan-summary notices that every render now ends with
// (#151). The count is still asserted: these tests are about one line's byte
// budget, so a second annotation would invalidate them, and simply taking
// out[0] would have hidden that.
func ghAnnotationLine(t *testing.T, out string) string {
	t.Helper()
	var annotations []string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !strings.HasPrefix(l, "::notice::formwork: ") {
			annotations = append(annotations, l)
		}
	}
	if len(annotations) != 1 {
		t.Fatalf("want exactly one annotation line, got:\n%s", out)
	}
	return annotations[0]
}

// TestGitHubTruncatesOverlongCure pins the workflow-command length cap
// (#107 review): GitHub truncates annotation lines past ~4096 chars
// SILENTLY, mid-cure — so the renderer budgets the whole LINE (prefix
// included) at 4096. The finding message is never truncated; the escaped
// cure portion is cut to fit and the cut is announced with a marker pointing
// at `formwork explain <rule-id>`.
func TestGitHubTruncatesOverlongCure(t *testing.T) {
	line := ghCureLine(t, "bad thing", strings.Repeat("x", 5000))
	if len(line) != 4096 {
		t.Fatalf("annotation is %d chars, want exactly the 4096 line budget:\n%.200s…", len(line), line)
	}
	if !strings.Contains(line, "::error file=x.go,line=3::bad thing (a)%0ACure: ") {
		t.Fatalf("finding message must survive untouched:\n%.200s…", line)
	}
	if !strings.HasSuffix(line, tpMarker) {
		t.Fatalf("truncated cure must end with the marker %q:\n…%s", tpMarker, line[len(line)-100:])
	}
}

// TestGitHubCureExactFullFitGetsNoMarker pins the full-fit boundary from the
// other side: when prefix+message+joiner+escaped-cure lands on exactly the
// 4096 line budget, the FULL cure appends plainly — a `<=`→`<` mutant in the
// full-fit comparison would divert an exactly-fitting cure into truncation
// with a misleading "truncated" marker.
func TestGitHubCureExactFullFitGetsNoMarker(t *testing.T) {
	cure := strings.Repeat("y", 200)
	raw := strings.Repeat("m", 4096-len(tpPrefix)-len(" (a)")-len(tpJoiner)-len(cure))
	line := ghCureLine(t, raw, cure)
	want := tpPrefix + raw + " (a)" + tpJoiner + cure
	if len(want) != 4096 {
		t.Fatalf("fixture arithmetic drifted: want-line is %d bytes, not 4096", len(want))
	}
	if line != want {
		t.Fatalf("an exactly-fitting cure must append in full with no marker,\ngot %d bytes: …%s", len(line), line[len(line)-120:])
	}
}

// TestGitHubCureBudgetBoundaries walks the exact edges of the per-annotation
// budget: at avail == ghMinCure (48) the fragment+marker append and the line
// lands on 4096 exactly; one byte below — and for every sliver-sized avail —
// NOTHING is appended: no fragment, no joiner, no marker. A 1–3 char cure
// sliver is noise, and a marker with no room of its own risks the cap.
func TestGitHubCureBudgetBoundaries(t *testing.T) {
	longCure := strings.Repeat("y", 200) // never fits untruncated here
	cases := []struct {
		name     string
		avail    int
		wantCure bool
	}{
		{"at minCure", 48, true},
		{"one below minCure", 47, false},
		{"sliver 3", 3, false},
		{"sliver 2", 2, false},
		{"sliver 1", 1, false},
		{"zero", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Repeat("m", tpRawMsgFor(tc.avail))
			line := ghCureLine(t, raw, longCure)
			if len(line) > 4096 {
				t.Fatalf("line is %d chars, over the 4096 budget", len(line))
			}
			bare := tpPrefix + raw + " (a)"
			if tc.wantCure {
				want := bare + tpJoiner + strings.Repeat("y", 48) + tpMarker
				if line != want {
					t.Fatalf("at avail==ghMinCure want exact 4096-char line with a 48-char fragment,\ngot  (%d chars): …%s", len(line), line[len(line)-120:])
				}
			} else if line != bare {
				t.Fatalf("below ghMinCure nothing may be appended — no fragment, no joiner, no marker,\ngot: …%s", line[len(line)-120:])
			}
		})
	}
}

// TestGitHubCureAppendSurvivesOldPanicVector reproduces the round-2 panic:
// under the old data-budget arithmetic this exact shape (escaped message data
// of 3946 bytes, rule "a", over-budget cure) produced avail==1, a non-'%' cut
// byte, strings.LastIndex == -1 passing the `-1 > len-3` guard, and a
// negative slice — a renderer panic reachable from `check --format github`.
func TestGitHubCureAppendSurvivesOldPanicVector(t *testing.T) {
	raw := strings.Repeat("m", 3942) // + " (a)" = 3946 bytes of message data
	line := ghCureLine(t, raw, strings.Repeat("x", 200))
	if len(line) > 4096 {
		t.Fatalf("line is %d chars, over the 4096 budget", len(line))
	}
	if !strings.Contains(line, raw+" (a)") {
		t.Fatalf("finding message must survive untouched:\n…%s", line[len(line)-120:])
	}
}

// TestGitHubOmitsCureWhenNoUsefulFragmentFits pins the omission rule: when
// the message (never modified) leaves no room for even a ghMinCure-sized
// fragment, the cure is omitted entirely — appending a marker that itself
// risks the silent cap would be worse than absence.
func TestGitHubOmitsCureWhenNoUsefulFragmentFits(t *testing.T) {
	t.Run("message exactly at the cap", func(t *testing.T) {
		raw := strings.Repeat("m", 4096-len(tpPrefix)-len(" (a)"))
		line := ghCureLine(t, raw, strings.Repeat("y", 200))
		if want := tpPrefix + raw + " (a)"; line != want {
			t.Fatalf("want the bare %d-char annotation with nothing appended, got %d chars:\n…%s", len(want), len(line), line[len(line)-120:])
		}
	})
	t.Run("message alone past the cap", func(t *testing.T) {
		raw := strings.Repeat("m", 4100)
		line := ghCureLine(t, raw, "short but real remediation")
		// The overflow is the message's own; the cure path must add nothing to it.
		if want := tpPrefix + raw + " (a)"; line != want {
			t.Fatalf("cure path must append nothing to an over-cap message, got %d chars:\n…%s", len(line), line[len(line)-120:])
		}
	})
}

// TestGitHubCureAccountsForPrefixLength pins that the budget is charged per
// annotation from the REAL `::level file=…,line=…::` prefix: with a 120-char
// path the same message+cure must still land within 4096 — a data-only budget
// with fixed headroom overflows here.
func TestGitHubCureAccountsForPrefixLength(t *testing.T) {
	path := strings.Repeat("p", 117) + ".go" // 120 chars
	raw := strings.Repeat("m", 3800)
	fs := []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: path, Line: 3, Message: raw},
	}
	var buf bytes.Buffer
	report.GitHub(&buf, []*config.Rule{{ID: "a", Cure: strings.Repeat("x", 5000)}}, fs, report.ScanSummary{})
	line := ghAnnotationLine(t, buf.String())
	if len(line) != 4096 {
		t.Fatalf("line is %d chars, want exactly 4096 — the prefix must be charged against the budget", len(line))
	}
	if !strings.Contains(line, "file="+path+",line=3::"+raw+" (a)") {
		t.Fatalf("prefix or message damaged:\n%.200s…", line)
	}
	if !strings.HasSuffix(line, tpMarker) {
		t.Fatalf("truncated cure must end with the marker:\n…%s", line[len(line)-120:])
	}
}

// TestGitHubCureCutNeverSeversEscapeOrRune pins the backoff at the cut point:
// a cut landing inside a %XX escape or a multi-byte rune backs off to the
// last clean boundary instead of emitting severed bytes before the marker.
// avail is 51 so the backed-off fragments (49 and 50 bytes) stay at or above
// the ghMinCure floor — the floor itself is pinned separately below.
func TestGitHubCureCutNeverSeversEscapeOrRune(t *testing.T) {
	raw := strings.Repeat("m", tpRawMsgFor(51)) // cut lands at byte 51 of the escaped cure
	t.Run("severed escape", func(t *testing.T) {
		// escaped form: 49 y's + "%25" + z's — byte 51 splits the %25.
		line := ghCureLine(t, raw, strings.Repeat("y", 49)+"%"+strings.Repeat("z", 100))
		if want := tpJoiner + strings.Repeat("y", 49) + tpMarker; !strings.HasSuffix(line, want) {
			t.Fatalf("cut must back off to before the severed %%25:\n…%s", line[len(line)-120:])
		}
	})
	t.Run("severed rune", func(t *testing.T) {
		// escaped form: 50 y's + 2-byte é + z's — byte 51 splits the é.
		line := ghCureLine(t, raw, strings.Repeat("y", 50)+"é"+strings.Repeat("z", 100))
		if want := tpJoiner + strings.Repeat("y", 50) + tpMarker; !strings.HasSuffix(line, want) {
			t.Fatalf("cut must back off to before the severed rune:\n…%s", line[len(line)-120:])
		}
	})
}

// TestGitHubCureOmittedWhenBackoffErodesBelowFloor pins the backoff floor
// with a cure that is not valid UTF-8 (raw 0x80 bytes — impossible from
// yaml.v3 today, unguarded here): the bytes at the cut point trim away as a
// severed-rune tail, the surviving fragment (45 bytes) is below ghMinCure,
// and the omission rule applies — the bare annotation comes back unchanged,
// never a joiner+marker wrapped around near-empty cure content.
func TestGitHubCureOmittedWhenBackoffErodesBelowFloor(t *testing.T) {
	raw := strings.Repeat("m", tpRawMsgFor(48))
	cure := strings.Repeat("y", 45) + "\x80\x80\x80" + strings.Repeat("z", 200)
	line := ghCureLine(t, raw, cure)
	if want := tpPrefix + raw + " (a)"; line != want {
		t.Fatalf("fragment below ghMinCure after backoff must be omitted entirely,\ngot %d bytes: …%q", len(line), line[len(line)-120:])
	}
}

func TestGitHubReport(t *testing.T) {
	var buf bytes.Buffer
	report.GitHub(&buf, nil, sampleFindings(), report.ScanSummary{})
	out := buf.String()
	for _, want := range []string{
		"::error file=x.go,line=3::bad thing (a)",
		"::warning::scope msg (w)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The suppressed finding surfaces ONLY at notice level (#91) — never as an
	// error or warning, which would read as a failure.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "hidden") && !strings.HasPrefix(line, "::notice") {
			t.Fatalf("suppressed finding leaked at non-notice level: %q", line)
		}
	}
}

// TestGitHubNamesSuppressedFindingsAsNotices pins #91's PR-lane half: each
// suppressed finding becomes one ::notice annotation naming the finding, the
// rule, and the suppression channel — after every live annotation, since
// reviewers read failures first.
func TestGitHubNamesSuppressedFindingsAsNotices(t *testing.T) {
	fs := []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "x.go", Line: 3, Message: "bad thing"},
		{RuleID: "b", Severity: finding.SeverityError, Path: "y,z.go", Line: 7, Message: "hidden", Suppressed: true, SuppressedBy: "allowlist:allow.txt:3"},
		{RuleID: "c", Severity: finding.SeverityError, Message: "scope-wide waived", Suppressed: true, SuppressedBy: "marker"},
	}
	var buf bytes.Buffer
	report.GitHub(&buf, nil, fs, report.ScanSummary{})
	out := buf.String()
	for _, want := range []string{
		"::notice file=y%2Cz.go,line=7::suppressed: hidden (b; allowlist:allow.txt:3)",
		"::notice::suppressed: scope-wide waived (c; marker)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	ei, ni := strings.Index(out, "::error"), strings.Index(out, "::notice")
	if ei < 0 || ni < 0 {
		t.Fatalf("both levels must be present (Index -1 would make the ordering check vacuous):\n%s", out)
	}
	if ni < ei {
		t.Fatalf("notices must follow live annotations:\n%s", out)
	}
}

// TestGitHubNoticesFollowLiveEvenWhenSuppressedSortsFirst pins the "after the
// live annotations" contract against the regression the simple fixtures can't
// see: engine sort order can legitimately put a suppressed finding FIRST in
// the input slice, and a renderer that collapsed the two loops into one
// interleaved pass would emit its notice first — byte-identical output on
// live-first fixtures, spec violation on this one.
func TestGitHubNoticesFollowLiveEvenWhenSuppressedSortsFirst(t *testing.T) {
	fs := []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "a.go", Line: 1, Message: "waived early", Suppressed: true, SuppressedBy: "marker"},
		{RuleID: "b", Severity: finding.SeverityError, Path: "b.go", Line: 2, Message: "live late"},
	}
	var buf bytes.Buffer
	report.GitHub(&buf, nil, fs, report.ScanSummary{})
	out := buf.String()
	le, fn := strings.LastIndex(out, "::error"), strings.Index(out, "::notice")
	if le < 0 || fn < 0 {
		t.Fatalf("both levels must be present:\n%s", out)
	}
	if fn < le {
		t.Fatalf("suppressed-first input must still render every live annotation before any notice:\n%s", out)
	}
}

// No suppressed findings means no SUPPRESSED notices. It no longer means no
// notices at all: the scan summary is emitted unconditionally, which is the
// point of #151 — this renderer used to write zero bytes for a run with nothing
// to report, so the surface adopters read said nothing about a scan that may
// have looked at nothing. The narrower assertion is what the test was always
// about; the broader one was only ever true by accident.
func TestGitHubEmitsNoSuppressedNoticesWhenNoneSuppressed(t *testing.T) {
	fs := []finding.Finding{
		{RuleID: "a", Severity: finding.SeverityError, Path: "x.go", Line: 3, Message: "bad thing"},
	}
	var buf bytes.Buffer
	report.GitHub(&buf, nil, fs, report.ScanSummary{})
	if strings.Contains(buf.String(), "suppressed:") {
		t.Fatalf("no suppressed findings, no suppressed notices:\n%s", buf.String())
	}
}

// The unconditional half of the same contract, pinned on its own: a run with
// no findings at all still says what it looked at.
func TestGitHubEmitsScanSummaryWithNoFindings(t *testing.T) {
	var buf bytes.Buffer
	report.GitHub(&buf, nil, nil, report.ScanSummary{FilesScanned: 7, RulesMatchingNoFiles: []string{"a"}})
	out := buf.String()
	for _, want := range []string{"::notice::formwork: 7 file(s) scanned", "::notice::formwork: a: scope matched no files"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDispatch(t *testing.T) {
	rls := []*config.Rule{rule("a")}
	var h, j, g bytes.Buffer
	if err := report.Render("", &h, rls, nil, report.ScanSummary{}); err != nil || !strings.Contains(h.String(), "[a] OK") {
		t.Fatalf("human default: %v %q", err, h.String())
	}
	if err := report.Render("json", &j, rls, nil, report.ScanSummary{}); err != nil || !strings.Contains(j.String(), `"rules_total": 1`) {
		t.Fatalf("json: %v %q", err, j.String())
	}
	if err := report.Render("github", &g, rls, sampleFindings(), report.ScanSummary{}); err != nil {
		t.Fatalf("github: %v", err)
	}
	if err := report.Render("xml", &bytes.Buffer{}, rls, nil, report.ScanSummary{}); err == nil {
		t.Fatal("unknown format must error")
	}
}
