package cli_test

import (
	"strings"
	"testing"
)

// An --rule value with no ids in it selects nothing, and nothing selected means
// the default: every configured rule runs. threeRuleFixtureRepo's r3 fails by
// construction, so "r3 appears in the output" proves the full set ran and the
// exit must equal the no-flag run's.
func TestTestRuleEmptySelectionRunsEveryRule(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	baseCode, baseOut, _ := runCLI(t, "test", "-C", root)
	if !strings.Contains(baseOut, "r3") {
		t.Fatalf("baseline no-flag run did not reach r3:\n%s", baseOut)
	}
	for _, v := range []string{"", ",", " , "} {
		code, out, errOut := runCLI(t, "test", "-C", root, "--rule", v)
		if code != baseCode {
			t.Errorf("--rule %q: exit = %d, want %d (the no-flag run)\nstderr:\n%s", v, code, baseCode, errOut)
		}
		for _, id := range []string{"r1", "r2", "r3"} {
			if !strings.Contains(out, "["+id+"]") {
				t.Errorf("--rule %q: %s did not run:\n%s", v, id, out)
			}
		}
	}
}

func TestTestRuleEmptySegmentsAndSpacingKeepExactSelection(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	for _, v := range []string{"r1,,r2", " r1 , r2 "} {
		code, out, _ := runCLI(t, "test", "-C", root, "--rule", v)
		assertRan(t, code, out, "r1", "r2")
	}
}

func TestTestRuleEmptyOccurrenceDoesNotWidenSelection(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--rule", "r1", "--rule", "")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "[r1] OK") {
		t.Errorf("r1 did not run:\n%s", out)
	}
	if strings.Contains(out, "r2") || strings.Contains(out, "r3") {
		t.Errorf("an empty occurrence widened the selection:\n%s", out)
	}
	if !strings.Contains(out, "1/1 rules passed") {
		t.Errorf("summary should count r1 alone:\n%s", out)
	}
}
