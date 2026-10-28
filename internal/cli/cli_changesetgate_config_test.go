package cli_test

// A rule's own definition is an input no scope glob names, so a change under
// .formwork/ has to be judged by what `check` reads there: formwork.yaml, which
// every rule is judged under, and the rule file that defines each rule. Nothing
// else under .formwork/ reaches a check verdict. The walk never enters the
// directory, and fixtures, mutation specs and the durations hint are data for
// other subcommands. Running every invariant on such a change cost a 4,300-rule
// corpus 494 s against 33 s for a docs edit.

import (
	"path/filepath"
	"testing"
)

const otherGateRules = `rules:
  - id: other-token-must-exist
    type: required-pattern
    scope: {include: ['**/*.go']}
    params:
      pattern: OTHER_TREE_TOKEN
      mode: exists
`

// twoFileRedRepo commits two rule files, each defining one invariant that fails
// at HEAD. Nothing is staged on return.
func twoFileRedRepo(t *testing.T) string {
	t.Helper()
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "other.yaml"), otherGateRules)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "second rule file")
	return dir
}

func TestStagedSkipsAnInvariantWhenOnlyAFixtureChanges(t *testing.T) {
	dir := redInvariantRepo(t)
	mustWrite(t, filepath.Join(dir, ".formwork", "fixtures", "token-must-exist", "fire-1", "keeper.go"), "package p\n")
	gitRun(t, dir, "add", ".formwork/fixtures")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "-format", "json")
	if code != 0 {
		t.Fatalf("a fixture-only change cannot move a check verdict, so the invariant must not run (exit 0), got %d:\n%s%s", code, out, errOut)
	}
	if rep := decodeNotRun(t, out); len(rep) != 1 || rep[0].Rule != "token-must-exist" || rep[0].Channel != "changeset" {
		t.Fatalf("the skipped invariant must be named on the changeset channel: %+v\n%s", rep, out)
	}
}

func TestStagedRunsOnlyTheRulesOfTheEditedRuleFile(t *testing.T) {
	dir := twoFileRedRepo(t)
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "other.yaml"), "# edited\n"+otherGateRules)
	gitRun(t, dir, "add", ".formwork/rules/other.yaml")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "-format", "json")
	if code != 1 {
		t.Fatalf("editing a rule's own file must run that rule (exit 1), got %d:\n%s%s", code, out, errOut)
	}
	if rep := decodeNotRun(t, out); len(rep) != 1 || rep[0].Rule != "token-must-exist" || rep[0].Channel != "changeset" {
		t.Fatalf("the rule in the untouched file must be skipped and named: %+v\n%s", rep, out)
	}
}

func TestStagedRunsEveryInvariantWhenFormworkYAMLChanges(t *testing.T) {
	dir := twoFileRedRepo(t)
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n# edited\n")
	gitRun(t, dir, "add", ".formwork/formwork.yaml")

	code, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "-format", "json")
	if code != 1 {
		t.Fatalf("a formwork.yaml change must run every invariant (exit 1), got %d:\n%s%s", code, out, errOut)
	}
	if rep := decodeNotRun(t, out); len(rep) != 0 {
		t.Fatalf("a formwork.yaml change must skip nothing: %+v\n%s", rep, out)
	}
}
