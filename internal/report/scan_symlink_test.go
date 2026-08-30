// scan_symlink_test.go — ScanSummary.UnfollowedLinks in each renderer (#309).
//
// The field is constructed directly here rather than driven from a walk: these
// tests own the RENDERING contract, and internal/cli's
// TestCheckDisclosesTheSymlinkItDeclinedToFollow owns the wiring that fills it.
// Split that way because the two failed independently — the record existed and
// was rendered for a year in `lint` while `check` never received it — so a
// single end-to-end test would have left either half free to disappear behind
// the other.
package report_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/report"
)

// TestEveryRendererNamesAnUnfollowedSymlink — one assertion per output format,
// for the reason the sibling self-skip test gives: an optional disclosure is
// one an adopter's CI does not have, and github is the surface adopters read.
func TestEveryRendererNamesAnUnfollowedSymlink(t *testing.T) {
	sum := report.ScanSummary{FilesScanned: 4, UnfollowedLinks: []string{"src/Program.cs"}}
	want := "scan: symlink not followed: src/Program.cs (skipped, and nothing under it scanned — formwork never follows links)"

	var human strings.Builder
	report.Human(&human, nil, nil, sum)
	if !strings.Contains(human.String(), want) {
		t.Errorf("human renderer dropped the unfollowed link:\n%s", human.String())
	}

	var gh strings.Builder
	report.GitHub(&gh, nil, nil, sum)
	if !strings.Contains(gh.String(), "::notice::formwork: "+want) {
		t.Errorf("github renderer dropped the unfollowed link:\n%s", gh.String())
	}

	var js strings.Builder
	report.JSON(&js, nil, nil, sum)
	var rep struct {
		Scan struct {
			Unfollowed []string `json:"unfollowed_symlinks"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.Unfollowed) != 1 || rep.Scan.Unfollowed[0] != "src/Program.cs" {
		t.Errorf("json renderer dropped the unfollowed link: %v\n%s", rep.Scan.Unfollowed, js.String())
	}
}

// TestNoRendererInventsAnUnfollowedSymlink is the control for all three arms at
// once. A disclosure emitted unconditionally is worse than none: it appears on
// every clean repository, and a reader who learns to skip the block loses the
// one run where it mattered.
func TestNoRendererInventsAnUnfollowedSymlink(t *testing.T) {
	sum := report.ScanSummary{FilesScanned: 4}

	var human strings.Builder
	report.Human(&human, nil, nil, sum)
	if strings.Contains(human.String(), "symlink not followed") {
		t.Errorf("human renderer invented a skip:\n%s", human.String())
	}

	var gh strings.Builder
	report.GitHub(&gh, nil, nil, sum)
	if strings.Contains(gh.String(), "symlink not followed") {
		t.Errorf("github renderer invented a skip:\n%s", gh.String())
	}

	var js strings.Builder
	report.JSON(&js, nil, nil, sum)
	// Present and empty, never absent or null: a consumer distinguishing "none"
	// from "this build does not report it" should not have to.
	if !strings.Contains(js.String(), `"unfollowed_symlinks": []`) {
		t.Errorf("json renderer must encode an empty list as []:\n%s", js.String())
	}
}

// TestUnfollowedSymlinkCapIsDisclosedAndJSONIsNotCapped holds both halves of
// detailListCap's contract on this list, because they are one decision: the
// line renderers truncate for a reader's sake and SAY they did, and the machine
// consumer gets everything. A truncation that does not announce itself reads as
// "that was all of them", which on THIS list is a false statement about how
// much of the tree went unread.
func TestUnfollowedSymlinkCapIsDisclosedAndJSONIsNotCapped(t *testing.T) {
	links := make([]string, 25)
	for i := range links {
		links[i] = fmt.Sprintf("src/link%02d.cs", i)
	}
	sum := report.ScanSummary{FilesScanned: 1, UnfollowedLinks: links}

	var human strings.Builder
	report.Human(&human, nil, nil, sum)
	got := strings.Count(human.String(), "symlink not followed:")
	if got != 10 {
		t.Errorf("human renderer named %d links, want detailListCap = 10:\n%s", got, human.String())
	}
	if !strings.Contains(human.String(), "… and 15 more symlink(s) not followed") {
		t.Errorf("the cap must say exactly how many it dropped:\n%s", human.String())
	}
	// The referral has to point somewhere the rest can actually be read, or it
	// is a second silent truncation wearing a signpost.
	if !strings.Contains(human.String(), "-format json names every one") {
		t.Errorf("the overflow line must point at a surface holding the rest:\n%s", human.String())
	}

	var js strings.Builder
	report.JSON(&js, nil, nil, sum)
	var rep struct {
		Scan struct {
			Unfollowed []string `json:"unfollowed_symlinks"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(js.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.Unfollowed) != 25 {
		t.Fatalf("json named %d links, want all 25 — the cap is a readability measure, not a contract\n%s", len(rep.Scan.Unfollowed), js.String())
	}
}

// TestUnfollowedSymlinkIsNotReportedAsADeclaredChannel pins the structural
// decision the field exists to express, and it is the one a later refactor is
// most likely to undo — "these are both things the walk removed, fold them
// together". A PruneChannel is one entry the operator DECLARED and what it
// removed; folding an undeclared skip into that list would tell an operator
// they wrote something they did not, and would need a glob and a reason that do
// not exist to render at all.
func TestUnfollowedSymlinkIsNotReportedAsADeclaredChannel(t *testing.T) {
	var sb strings.Builder
	report.JSON(&sb, nil, nil, report.ScanSummary{
		FilesScanned:    1,
		UnfollowedLinks: []string{"src/Program.cs"},
	})
	var rep struct {
		Scan struct {
			Prunes []struct {
				Channel string `json:"channel"`
			} `json:"prune_channels"`
			Unfollowed []string `json:"unfollowed_symlinks"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(sb.String()), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Scan.Prunes) != 0 {
		t.Errorf("an undeclared skip must not appear as a declared prune channel: %+v\n%s", rep.Scan.Prunes, sb.String())
	}
	if len(rep.Scan.Unfollowed) != 1 {
		t.Errorf("...and it must still be disclosed, in its own field: %v\n%s", rep.Scan.Unfollowed, sb.String())
	}
}
