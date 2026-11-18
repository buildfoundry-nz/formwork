// cli_rulesfile_test.go — `check --rules-file PATH` runs exactly the rule ids
// the file lists (TakeoffQS#17436): a pull request's CI runs only the rules
// its change can affect, decided once by a selector outside the engine. The
// engine's half is to run that list exactly — never a superset it was not
// asked for, never a subset that silently drops an id — and to say in the
// report that the run was a selection, so nothing downstream mistakes a part
// of the corpus for the whole.
package cli_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type rulesFileReport struct {
	Findings []struct {
		Rule string `json:"rule"`
	} `json:"findings"`
	Scan struct {
		Selection *struct {
			Listed int `json:"listed"`
			Lane   int `json:"lane"`
		} `json:"selection"`
		Shard *struct {
			Rules []string `json:"rules"`
		} `json:"shard"`
	} `json:"scan"`
	Summary struct {
		RulesTotal  int `json:"rules_total"`
		RulesPassed int `json:"rules_passed"`
	} `json:"summary"`
	Durations map[string]int `json:"durations"`
}

func writeRulesFile(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(root, "selected.txt")
	mustWrite(t, p, body)
	return p
}

func TestCheckRulesFileRunsExactlyTheListedRules(t *testing.T) {
	root := shardRepo(t)
	// no-alpha fires, no-beta does not; no-delta fires but is not listed.
	list := writeRulesFile(t, root, "# selected for this change\nno-alpha\n\nno-beta\n")
	code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", list, "-format", "json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (no-alpha fires)\nstderr:\n%s", code, errOut)
	}
	var rep rulesFileReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if rep.Summary.RulesTotal != 2 {
		t.Fatalf("rules_total = %d, want 2 — the run must judge exactly the listed rules\n%s", rep.Summary.RulesTotal, out)
	}
	for _, f := range rep.Findings {
		if f.Rule != "no-alpha" {
			t.Fatalf("finding from %q, a rule the file did not list:\n%s", f.Rule, out)
		}
	}
	if len(rep.Findings) == 0 {
		t.Fatalf("no-alpha was listed and fires, but reported nothing:\n%s", out)
	}
	if rep.Scan.Selection == nil || rep.Scan.Selection.Listed != 2 || rep.Scan.Selection.Lane != 6 {
		t.Fatalf("the report must say the run was a selection of 2 from a 6-rule lane:\n%s", out)
	}
}

// Without the flag the report must not claim a selection: a whole-lane run
// is the backstop every selected run leans on, and it must read as one.
func TestCheckWithoutRulesFileReportsNoSelection(t *testing.T) {
	root := shardRepo(t)
	_, out, _ := runCLI(t, "check", "-C", root, "--lane", "ci", "-format", "json")
	if strings.Contains(out, `"selection"`) {
		t.Fatalf("a whole-lane run reported a selection:\n%s", out)
	}
}

func TestCheckRulesFileUnknownIDIsRefused(t *testing.T) {
	root := shardRepo(t)
	list := writeRulesFile(t, root, "no-alpha\nno-such-rule\n")
	code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", list)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 — a listed id the lane does not hold must not shrink the run silently\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "no-such-rule") || !strings.Contains(errOut, "--rules-file") {
		t.Fatalf("the refusal must name the flag and the id:\n%s", errOut)
	}
}

// A listed rule the lane does not select is refused, not run: the file narrows
// the lane, it never widens it.
func TestCheckRulesFileIDOutsideTheLaneIsRefused(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\nlanes:\n  ci: {all: true}\n  narrow: {tags: [narrow]}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"), "rules:\n"+
		"  - id: no-alpha\n    type: forbidden-pattern\n    tags: [narrow]\n    scope: {include: ['**/*.go']}\n    params: {pattern: ALPHA}\n"+
		"  - id: no-beta\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: BETA}\n")
	mustWrite(t, filepath.Join(root, "src", "bad.go"), "const x = \"ALPHA\"\n")
	list := writeRulesFile(t, root, "no-alpha\nno-beta\n")
	code, _, errOut := runCLI(t, "check", "-C", root, "--lane", "narrow", "--rules-file", list)
	if code != 2 || !strings.Contains(errOut, "no-beta") {
		t.Fatalf("exit = %d, want 2 naming no-beta\nstderr:\n%s", code, errOut)
	}
}

func TestCheckRulesFileThatListsNothingIsRefused(t *testing.T) {
	root := shardRepo(t)
	for name, body := range map[string]string{"empty": "", "comments only": "# none\n\n"} {
		list := writeRulesFile(t, root, body)
		code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", list)
		if code != 2 {
			t.Fatalf("%s: exit = %d, want 2 — a run over no rules is not a pass\nstdout:\n%s\nstderr:\n%s", name, code, out, errOut)
		}
		if !strings.Contains(errOut, "--rules-file") {
			t.Fatalf("%s: the refusal must name the flag:\n%s", name, errOut)
		}
	}
}

func TestCheckRulesFileEmptyValueAndUnreadableFileAreRefused(t *testing.T) {
	root := shardRepo(t)
	for _, v := range []string{"", filepath.Join(root, "absent.txt")} {
		code, _, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", v)
		if code != 2 || !strings.Contains(errOut, "--rules-file") {
			t.Fatalf("--rules-file %q: exit = %d, want 2 naming the flag\nstderr:\n%s", v, code, errOut)
		}
	}
}

// --shard partitions the selection, so the N shard reports together cover the
// listed rules exactly once and nothing else.
func TestCheckRulesFileShardsCoverTheSelectionOnce(t *testing.T) {
	root := shardRepo(t)
	listed := []string{"no-alpha", "no-delta", "no-epsilon", "no-zeta"}
	list := writeRulesFile(t, root, strings.Join(listed, "\n")+"\n")
	for n := 1; n <= 3; n++ {
		var union []string
		for k := 1; k <= n; k++ {
			spec := strconv.Itoa(k) + "/" + strconv.Itoa(n)
			code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", list, "--shard", spec, "-format", "json")
			if code != 0 && code != 1 {
				t.Fatalf("--shard %s: exit = %d\nstderr:\n%s", spec, code, errOut)
			}
			var rep rulesFileReport
			if err := json.Unmarshal([]byte(out), &rep); err != nil {
				t.Fatalf("--shard %s: %v\n%s", spec, err, out)
			}
			if rep.Scan.Shard == nil || rep.Scan.Selection == nil || rep.Scan.Selection.Listed != len(listed) {
				t.Fatalf("--shard %s: a sharded selection must report both:\n%s", spec, out)
			}
			union = append(union, rep.Scan.Shard.Rules...)
		}
		slices.Sort(union)
		if !slices.Equal(union, listed) {
			t.Fatalf("n=%d: shards ran %v, want each of %v exactly once", n, union, listed)
		}
	}
}

// Duplicated ids are one rule: listing an id twice must not run it twice or
// count it twice.
func TestCheckRulesFileDuplicateIDRunsOnce(t *testing.T) {
	root := shardRepo(t)
	list := writeRulesFile(t, root, "no-beta\nno-beta\n")
	code, out, errOut := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", list, "-format", "json")
	if code != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", code, errOut)
	}
	var rep rulesFileReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if rep.Summary.RulesTotal != 1 || rep.Scan.Selection == nil || rep.Scan.Selection.Listed != 1 {
		t.Fatalf("a duplicated id must count once:\n%s", out)
	}
}

func TestCheckRulesFileHeadlineSaysItIsASelection(t *testing.T) {
	root := shardRepo(t)
	list := writeRulesFile(t, root, "no-beta\n")
	_, out, _ := runCLI(t, "check", "-C", root, "--lane", "ci", "--rules-file", list)
	if !strings.Contains(out, "1 of 6 lane rule(s) selected by --rules-file") {
		t.Fatalf("the headline must say the run judged a selection:\n%s", out)
	}
}
