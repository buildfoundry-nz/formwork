package cli_test

// A whole-tree invariant evaluates over the tracked tree under --staged/--range
// (#4), and a census detector reads the whole root, so before this change every
// one of them ran on every commit, whatever the commit touched. On a 4,300-rule corpus that was 1,413 whole-tree rules and
// ~335 CPU-seconds for a commit that changed one file no invariant reads.
//
// A scope-bound invariant's verdict is a function of the files its scope admits.
// A changeset that adds, edits, deletes or renames none of them cannot move that
// verdict, so the rule is not run and is NAMED in rules_not_run on its own
// channel. These tests pin both halves: the skip, and every way a change reaches
// a rule's inputs and must still run it.
//
// Each repo below is RED on its invariant at HEAD: keeper.go lacks the token.
// That is what makes a skip observable as an exit code — a run that evaluated
// the rule exits 1, a run that skipped it exits 0.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const gateRules = `rules:
  - id: token-must-exist
    type: required-pattern
    scope: {include: ['**/*.go']}
    params:
      pattern: WHOLE_TREE_TOKEN
      mode: exists
`

// redInvariantRepo commits a tree whose only invariant fails: no .go file bears
// WHOLE_TREE_TOKEN. Nothing is staged on return.
func redInvariantRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), gateRules)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "seed\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	return dir
}

func TestStagedSkipsAnInvariantTheChangesetCannotReach(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, "README.md"), "edited\n")
	gitRun(t, dir, "add", "README.md")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "-format", "json")
	if code != 0 {
		t.Fatalf("a staged change outside the invariant's scope must not run it (exit 0), got %d:\n%s%s", code, out, errOut)
	}
	rep := decodeNotRun(t, out)
	if len(rep) != 1 || rep[0].Rule != "token-must-exist" || rep[0].Channel != "changeset" {
		t.Fatalf("the skipped invariant must be named on the changeset channel: %+v\n%s", rep, out)
	}
	if rep[0].Reason == "" {
		t.Fatalf("the skip must say why: %+v", rep[0])
	}
}

func TestRangeSkipsAnInvariantTheRangeCannotReach(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, "README.md"), "edited\n")
	gitRun(t, dir, "commit", "-q", "-am", "docs only")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--range", "HEAD~1..HEAD")
	if code != 0 {
		t.Fatalf("a range outside the invariant's scope must not run it (exit 0), got %d:\n%s%s", code, out, errOut)
	}
}

func TestStagedRunsAnInvariantWhenTheChangesetEditsItsScope(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n\n// edited\n")
	gitRun(t, dir, "add", "keeper.go")

	if code, out, errOut := runCLI(t, "check", "-C", dir, "--staged"); code != 1 {
		t.Fatalf("an in-scope edit must run the invariant (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// A deletion is the case the scannable changeset drops: the path no longer
// exists to open. Removing the file that bore the token is exactly how an
// exists-mode invariant breaks, so it must count as reaching the scope.
func TestStagedRunsAnInvariantWhenTheChangesetDeletesFromItsScope(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), gateRules)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n")
	mustWrite(t, filepath.Join(dir, "bearer.go"), "package p\n\n// WHOLE_TREE_TOKEN\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	gitRun(t, dir, "rm", "-q", "bearer.go")

	if code, out, errOut := runCLI(t, "check", "-C", dir, "--staged"); code != 1 {
		t.Fatalf("deleting the token's only bearer must run the invariant (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// A rename moves a path out of scope as surely as a deletion does: the old end
// is in scope, the new end is not.
func TestStagedRunsAnInvariantWhenTheChangesetRenamesOutOfItsScope(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), gateRules)
	mustWrite(t, filepath.Join(dir, "keeper.go"), "package p\n")
	mustWrite(t, filepath.Join(dir, "bearer.go"), "package p\n\n// WHOLE_TREE_TOKEN\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	gitRun(t, dir, "mv", "bearer.go", "bearer.txt")

	if code, out, errOut := runCLI(t, "check", "-C", dir, "--staged"); code != 1 {
		t.Fatalf("renaming the token's only bearer out of scope must run the invariant (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// The rule's own definition is an input no scope glob names. A change under
// .formwork/ runs every invariant, whichever rule it edited.
func TestStagedRunsEveryInvariantWhenTheConfigChanges(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), "# edited\n"+gateRules)
	gitRun(t, dir, "add", ".formwork/rules/rules.yaml")

	if code, out, errOut := runCLI(t, "check", "-C", dir, "--staged"); code != 1 {
		t.Fatalf("a config change must run every invariant (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// invariant_rules counts what ran over the tracked tree. A skipped invariant
// did not, so it is not counted there; it is counted in rules_not_run.
func TestStagedInvariantCountExcludesTheSkipped(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, "README.md"), "edited\n")
	gitRun(t, dir, "add", "README.md")

	_, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "-format", "json")
	if n := decodeInvariantCount(t, out); n != 0 {
		t.Fatalf("invariant_rules = %d, want 0: the one invariant was skipped\n%s%s", n, out, errOut)
	}
}

func decodeInvariantCount(t *testing.T, out string) int {
	t.Helper()
	var rep struct {
		Scan struct {
			Invariant int `json:"invariant_rules"`
		} `json:"scan"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	return rep.Scan.Invariant
}

const censusRules = `rules:
  - id: census-fires
    type: forbidden-pattern
    scope: {include: ['**/*.go']}
    params:
      census:
        cmd: [sh, -c, 'echo folded-document; exit 1']
`

// censusRepo commits a tree whose census always fires. Nothing is staged.
func censusRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), censusRules)
	mustWrite(t, filepath.Join(dir, "a.go"), "package p\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "seed\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	return dir
}

// A census is a forbidden-pattern rule: on a changeset it judges what the
// changeset reached in its scope. One that reached none has nothing to judge,
// and its whole-root detector is not run.
func TestStagedSkipsACensusTheChangesetCannotReach(t *testing.T) {
	dir := censusRepo(t)
	mustWrite(t, filepath.Join(dir, "README.md"), "edited\n")
	gitRun(t, dir, "add", "README.md")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "--skip-escapes", "-format", "json")
	if code != 0 {
		t.Fatalf("a staged change outside the census scope must not run its detector (exit 0), got %d:\n%s%s", code, out, errOut)
	}
	rep := decodeNotRun(t, out)
	if len(rep) != 1 || rep[0].Rule != "census-fires" || rep[0].Channel != "changeset" {
		t.Fatalf("the skipped census must be named on the changeset channel: %+v\n%s", rep, out)
	}
}

func TestStagedRunsACensusWhenTheChangesetReachesItsScope(t *testing.T) {
	dir := censusRepo(t)
	mustWrite(t, filepath.Join(dir, "a.go"), "package p\n\n// edited\n")
	gitRun(t, dir, "add", "a.go")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "--skip-escapes")
	if code != 1 || !strings.Contains(out, "folded-document") {
		t.Fatalf("an in-scope change must run the census and report it (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// The whole-tree run is the backstop every skip names, so it must never gate.
func TestWholeTreeRunsACensusWhateverChanged(t *testing.T) {
	dir := censusRepo(t)
	if code, out, errOut := runCLI(t, "check", "-C", dir, "--skip-escapes"); code != 1 {
		t.Fatalf("a whole-tree run must run the census (exit 1), got %d:\n%s%s", code, out, errOut)
	}
}

// A rule the gate did not run reached no verdict, so it must not read as one:
// no `[id] OK`, and not counted among the rules that passed.
func TestStagedSkipIsNotRenderedAsAPass(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, "README.md"), "edited\n")
	gitRun(t, dir, "add", "README.md")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s%s", code, out, errOut)
	}
	if strings.Contains(out, "[token-must-exist] OK") {
		t.Fatalf("a skipped rule must not render as OK:\n%s", out)
	}
	if !strings.Contains(out, "0/0 rules passed") {
		t.Fatalf("a skipped rule must not be counted as passed:\n%s", out)
	}
	if !strings.Contains(out, "token-must-exist: did not run") {
		t.Fatalf("the skip must be named in the scan block:\n%s", out)
	}
}
