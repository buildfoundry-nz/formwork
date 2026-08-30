package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// #159: a `command` rule carrying `when.paths_changed` runs its tool only when a
// file in its own scope matched the trigger. The skip itself is correct — it is what
// `when:` is for, and it keeps the pre-commit hook fast — but it rendered as
// `[id] OK`, byte-identical to a rule that ran its tool and passed. The exit
// code is unchanged by this fix; only the silence is.
//
// The pair below is the reproduction from the issue, and it is a pair on
// purpose: a disclosure that fires for every command rule would be worthless,
// so the whole-tree half asserts the rule that DID run is not named.

func commandSkipRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: migrations-gate\n    type: command\n    scope: {include: ['**']}\n"+
			"    params:\n      cmd: [\"false\"]\n      when: {paths_changed: ['db/**']}\n      expect: {exit: 0}\n")
	mustWrite(t, filepath.Join(root, "db", "001.sql"), "select 1;\n")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "package p\n")
	return root
}

// stageOnlySrc commits the whole tree, then stages a change to src/a.go alone —
// so the staged set holds nothing under db/ and the trigger cannot match.
func stageOnlySrc(t *testing.T, root string) {
	t.Helper()
	gitInit(t, root)
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "init")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "package p // edited\n")
	gitRun(t, root, "add", "src/a.go")
}

func TestCheckStagedNamesTheSelfSkippedCommandRule(t *testing.T) {
	root := commandSkipRepo(t)
	stageOnlySrc(t, root)

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the skip is correct, only its visibility changes\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "migrations-gate: skipped") {
		t.Fatalf("the self-skipped rule must be named in the scan summary:\n%s", out)
	}
	// The reason has to carry both halves of the cure: which gate declined, and
	// that the tool never ran. Without the second an operator reads the line as
	// "ran, matched nothing".
	for _, want := range []string{"when.paths_changed", "db/**", "did not run"} {
		if !strings.Contains(out, want) {
			t.Errorf("skip line missing %q:\n%s", want, out)
		}
	}
}

// The control. A rule that RAN is not named as skipped — and the tool's verdict
// is unchanged, which is what stops the disclosure from being satisfied by
// naming everything.
func TestCheckWholeTreeDoesNotNameACommandRuleThatRan(t *testing.T) {
	root := commandSkipRepo(t)

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (db/001.sql triggers the gate, which exits 1)\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "[migrations-gate] FAIL") {
		t.Fatalf("the triggered gate must still fail:\n%s", out)
	}
	// Same dead-guard hazard as TestNoSkipsRenderNoSkipLine: report's own words
	// for a not-run entry are "did not run", and "skipped" arrives only from the
	// checker's reason. Assert both, or a renderer that named this rule in words
	// of its own would slip past.
	for _, forbidden := range []string{"did not run", "skipped"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("a rule that ran its tool was reported with %q:\n%s", forbidden, out)
		}
	}
}

// A whole-tree run can self-skip too — nothing under db/ exists — and it is
// reported there as well. This is deliberately UNLIKE RulesMatchingNoFiles,
// which #160 populates only for whole-tree runs: vacuity is a whole-tree
// question, whereas a gate that declined to run is worth naming in whichever
// mode it declined in.
func TestCheckWholeTreeNamesTheSelfSkippedCommandRule(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: migrations-gate\n    type: command\n    scope: {include: ['**']}\n"+
			"    params:\n      cmd: [\"false\"]\n      when: {paths_changed: ['db/**']}\n      expect: {exit: 0}\n")
	mustWrite(t, filepath.Join(root, "src", "a.go"), "package p\n")

	code, out, errOut := runCLI(t, "check", "-C", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "migrations-gate: skipped") {
		t.Fatalf("a whole-tree self-skip must be named too:\n%s", out)
	}
}

// --skip-escapes is the OTHER way a command rule does not run: it is filtered
// out before the engine, so its checker never runs, never records a skip, and
// nothing in the summary could name it. The drop is legitimate and stays exit 0.
//
// It is pre-existing, and the disclosure above is what makes the silence
// misleading: before it the scan block never named a declined gate, so saying
// nothing carried no information — now an empty skip section reads as "no gate
// declined". Note the asymmetry it removes: dropping ALL escapes is already a
// named exit 2 one screen up, while dropping SOME was total silence.
//
// A rule the --lane selector did not choose is deliberately NOT reported here:
// that is selection working as asked, not a rule dropped out from under the run.
func TestCheckSkipEscapesNamesTheDroppedEscapeRules(t *testing.T) {
	root := commandSkipRepo(t)
	// A fast rule so the run has something left to do — with the escape dropped
	// and nothing else configured, the zero-rules refusal fires instead (exit 2,
	// TestCheckSkipEscapesEmptyingRuleSetExits2) and proves nothing about this.
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "fast.yaml"),
		"rules:\n  - id: no-widget\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: WIDGET}\n")
	stageOnlySrc(t, root)

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged", "--skip-escapes")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the drop is legitimate\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "migrations-gate") {
		t.Fatalf("the dropped escape must be named:\n%s", out)
	}
	if !strings.Contains(out, "--skip-escapes") {
		t.Errorf("the reason must name the flag that dropped it:\n%s", out)
	}
	if !strings.Contains(out, "[no-widget] OK") {
		t.Errorf("the fast rule must still have run:\n%s", out)
	}

	code, out, errOut = runCLI(t, "check", "-C", root, "--staged", "--skip-escapes", "-format", "json")
	if code != 0 {
		t.Fatalf("json: exit = %d, want 0\nstderr:\n%s", code, errOut)
	}
	rep := decodeNotRun(t, out)
	if len(rep) != 1 || rep[0].Rule != "migrations-gate" || rep[0].Channel != "skip-escapes" {
		t.Fatalf("the operator-narrowed drop must carry its own channel: %+v\n%s", rep, out)
	}
}

// notRun is the scan block's rules_not_run entry, decoded.
type notRun struct {
	Rule    string `json:"rule"`
	Channel string `json:"channel"`
	Reason  string `json:"reason"`
}

func decodeNotRun(t *testing.T, out string) []notRun {
	t.Helper()
	var rep struct {
		Scan struct {
			NotRun []notRun `json:"rules_not_run"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	return rep.Scan.NotRun
}

// The machine formats are what an adopter's CI reads, so the disclosure has to
// survive into them or it is one an adopter does not have.
func TestCheckSelfSkipReachesJSONAndGitHub(t *testing.T) {
	root := commandSkipRepo(t)
	stageOnlySrc(t, root)

	code, out, errOut := runCLI(t, "check", "-C", root, "--staged", "-format", "json")
	if code != 0 {
		t.Fatalf("json: exit = %d, want 0\nstderr:\n%s", code, errOut)
	}
	rep := decodeNotRun(t, out)
	if len(rep) != 1 || rep[0].Rule != "migrations-gate" ||
		!strings.Contains(rep[0].Reason, "when.paths_changed") {
		t.Fatalf("json scan block did not carry the skip: %+v\n%s", rep, out)
	}
	// The channel is what lets a consumer tell a checker's own gate from an
	// operator narrowing the run; string-matching the reason is not a contract.
	if rep[0].Channel != "self-skip" {
		t.Errorf("wrong channel for a checker's own skip: %+v", rep[0])
	}

	code, out, errOut = runCLI(t, "check", "-C", root, "--staged", "-format", "github")
	if code != 0 {
		t.Fatalf("github: exit = %d, want 0\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(out, "::notice::formwork: migrations-gate: skipped") {
		t.Fatalf("github annotations did not carry the skip:\n%s", out)
	}
}
