package report_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/report"
)

// The JSON consumer gets the WHOLE list even when the human renderer capped it:
// the cap is a readability measure, and a machine reading `rules_matching_no_
// files` to decide whether its rules are live must not silently receive a
// prefix.
func TestJSONVacuousRuleListIsNotCapped(t *testing.T) {
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = string(rune('a'+i/10)) + string(rune('0'+i%10))
	}
	var sb strings.Builder
	report.JSON(&sb, nil, nil, report.ScanSummary{FilesScanned: 3, RulesMatchingNoFiles: ids})
	var rep struct {
		Scan struct {
			RulesMatchingNoFiles []string `json:"rules_matching_no_files"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(sb.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.RulesMatchingNoFiles) != 25 {
		t.Fatalf("got %d ids, want all 25", len(rep.Scan.RulesMatchingNoFiles))
	}
}

// #159, one test per renderer: a rule whose checker declined to run itself is
// named with its reason wherever the summary is rendered. An optional
// disclosure is one an adopter's CI does not have, and the github format is the
// surface adopters most often read.
func TestEveryRendererNamesASelfSkippedRule(t *testing.T) {
	sum := report.ScanSummary{
		FilesScanned: 4,
		RulesNotRun: []report.SkippedRule{{
			RuleID: "migrations-gate",
			Reason: "skipped: when.paths_changed (db/**) matched no scanned file, so [false] did not run",
		}},
	}
	want := "migrations-gate: skipped: when.paths_changed (db/**) matched no scanned file, so [false] did not run"

	var human strings.Builder
	report.Human(&human, nil, nil, sum)
	if !strings.Contains(human.String(), want) {
		t.Errorf("human renderer dropped the skip:\n%s", human.String())
	}

	var gh strings.Builder
	report.GitHub(&gh, nil, nil, sum)
	if !strings.Contains(gh.String(), "::notice::formwork: "+want) {
		t.Errorf("github renderer dropped the skip:\n%s", gh.String())
	}

	var js strings.Builder
	report.JSON(&js, nil, nil, sum)
	var rep struct {
		Scan struct {
			SelfSkipped []struct {
				Rule   string `json:"rule"`
				Reason string `json:"reason"`
			} `json:"rules_not_run"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.SelfSkipped) != 1 || rep.Scan.SelfSkipped[0].Rule != "migrations-gate" ||
		rep.Scan.SelfSkipped[0].Reason != sum.RulesNotRun[0].Reason {
		t.Errorf("json renderer dropped the skip: %+v\n%s", rep.Scan.SelfSkipped, js.String())
	}
}

// Two kinds of thing stop a rule running and they are different in kind — a
// checker declining its own gate is the rule working as configured, while
// --skip-escapes is an operator narrowing this run. A CI alerting on the second
// but not the first must be able to tell them apart structurally, so both
// channels have to survive into JSON as distinct values.
func TestJSONSkipChannelsRoundTripDistinctly(t *testing.T) {
	var sb strings.Builder
	report.JSON(&sb, nil, nil, report.ScanSummary{
		FilesScanned: 1,
		RulesNotRun: []report.SkippedRule{
			{RuleID: "dropped-gate", Channel: report.SkipChannelSkipEscapes, Reason: "did not run: --skip-escapes dropped this heavy command rule"},
			{RuleID: "trigger-gate", Channel: report.SkipChannelSelf, Reason: "skipped: no file in this rule's scope matched when.paths_changed (db/**)"},
		},
	})
	var rep struct {
		Scan struct {
			NotRun []struct {
				Rule    string `json:"rule"`
				Channel string `json:"channel"`
			} `json:"rules_not_run"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(sb.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.NotRun) != 2 {
		t.Fatalf("got %d entries, want 2:\n%s", len(rep.Scan.NotRun), sb.String())
	}
	if rep.Scan.NotRun[0].Channel != "skip-escapes" || rep.Scan.NotRun[1].Channel != "self-skip" {
		t.Fatalf("the two channels did not round-trip distinctly: %+v\n%s", rep.Scan.NotRun, sb.String())
	}
	// The values are the consumer's contract, so they are pinned as literals
	// here: renaming a constant must break this test, not a downstream CI.
	if report.SkipChannelSelf == report.SkipChannelSkipEscapes {
		t.Fatal("the two channel constants collapsed to one value")
	}
}

// A skip whose checker gave no reason is the one most at risk of being dropped
// on the way out, and the one it would be worst to drop.
func TestSelfSkipWithNoReasonStillRendersALine(t *testing.T) {
	var human strings.Builder
	report.Human(&human, nil, nil, report.ScanSummary{
		FilesScanned: 1,
		RulesNotRun:  []report.SkippedRule{{RuleID: "mute-gate"}},
	})
	if !strings.Contains(human.String(), "mute-gate: did not run, and no reason was recorded") {
		t.Errorf("a reasonless skip must still name the rule, in words:\n%s", human.String())
	}
}

// A summary with no skips must say nothing about skips: an unconditional line
// in front of every clean run trains readers to skip the whole block.
func TestNoSkipsRenderNoSkipLine(t *testing.T) {
	var human strings.Builder
	report.Human(&human, nil, nil, report.ScanSummary{FilesScanned: 4})
	// Both spellings, because report's own vocabulary for these entries is "did
	// not run" — "skipped" now reaches the output only inside a checker's reason
	// string. Asserting the checker's word alone left this guard passing against
	// any renderer change that emitted the block unconditionally, which is the
	// test-that-cannot-fail shape (#152); it is asserted too so that a producer
	// wording its reason that way is still caught.
	for _, forbidden := range []string{"did not run", "skipped"} {
		if strings.Contains(human.String(), forbidden) {
			t.Errorf("a run with nothing skipped emitted %q:\n%s", forbidden, human.String())
		}
	}
}

// The line renderers cap the list the same way they cap the vacuous one, and
// the overflow line DISCLOSES the exact number dropped — a truncated list that
// does not say so reads as "that was all of them". It points at -format json,
// which carries the whole list; `formwork lint` (where the vacuous overflow
// sends readers) knows nothing about a skip taken at check time.
func TestSelfSkipListIsCappedWithADisclosedOverflow(t *testing.T) {
	var skips []report.SkippedRule
	for i := range 25 {
		skips = append(skips, report.SkippedRule{RuleID: "r" + string(rune('a'+i)), Reason: "skipped: x"})
	}
	sum := report.ScanSummary{FilesScanned: 1, RulesNotRun: skips}

	var human strings.Builder
	report.Human(&human, nil, nil, sum)
	if n := strings.Count(human.String(), "skipped: x"); n != 10 {
		t.Errorf("human renderer named %d skips, want the cap of 10:\n%s", n, human.String())
	}
	if !strings.Contains(human.String(), "15 more") || !strings.Contains(human.String(), "json") {
		t.Errorf("the overflow must state the number dropped and where the rest is:\n%s", human.String())
	}

	var js strings.Builder
	report.JSON(&js, nil, nil, sum)
	var rep struct {
		Scan struct {
			SelfSkipped []struct{} `json:"rules_not_run"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.SelfSkipped) != 25 {
		t.Errorf("json got %d skips, want all 25 — a machine consumer must not receive a silent prefix", len(rep.Scan.SelfSkipped))
	}
}

// The machine format is where an adopter's CI reads the requested-vs-scanned
// gap, so every field the summary carries must be pinned there. Without this,
// deleting all three new fields from toJSON passes the entire suite — verified
// by mutation, which is how the gap was found.
func TestJSONCarriesEveryScanSummaryField(t *testing.T) {
	var sb strings.Builder
	report.JSON(&sb, nil, nil, report.ScanSummary{
		FilesScanned: 2, PathsRequested: 5, FileSetMode: "--staged", InvariantRules: 3,
	})
	var rep struct {
		Scan struct {
			FilesScanned   int    `json:"files_scanned"`
			PathsRequested int    `json:"paths_requested"`
			FileSetMode    string `json:"file_set_mode"`
			InvariantRules int    `json:"invariant_rules"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(sb.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Scan.FilesScanned != 2 || rep.Scan.PathsRequested != 5 ||
		rep.Scan.FileSetMode != "--staged" || rep.Scan.InvariantRules != 3 {
		t.Fatalf("scan block lost a field: %+v\n%s", rep.Scan, sb.String())
	}
}
