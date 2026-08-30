package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// twoRuleFixtureRepo writes a repo with two well-formed rules, each with a
// passing fire and pass fixture, so a rule-filtered run can be observed to
// touch exactly one of them.
func twoRuleFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: no-alpha\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: alpha}\n"+
			"  - id: no-beta\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: beta}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-alpha", "fire-1", "f.txt"), "alpha want: no-alpha\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-alpha", "pass-1", "f.txt"), "clean\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-beta", "fire-1", "f.txt"), "beta want: no-beta\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "no-beta", "pass-1", "f.txt"), "clean\n")
	return root
}

func TestTestRuleFilterRunsOnlyThatRule(t *testing.T) {
	root := twoRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--rule", "no-alpha")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "[no-alpha] OK") {
		t.Errorf("output missing filtered rule verdict:\n%s", out)
	}
	if strings.Contains(out, "no-beta") {
		t.Errorf("unfiltered rule should not run:\n%s", out)
	}
	if !strings.Contains(out, "1/1 rules passed") {
		t.Errorf("summary should count only the filtered rule:\n%s", out)
	}
}

func TestTestRuleUnmatchedExits2(t *testing.T) {
	root := twoRuleFixtureRepo(t)
	code, _, errOut := runCLI(t, "test", "-C", root, "--rule", "no-such-rule")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (fail-closed on unmatched selector)\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "no-such-rule") {
		t.Errorf("stderr should name the unmatched rule:\n%s", errOut)
	}
}

// lintTwoRuleRepo writes a repo with one well-formed rule ("good") and one
// broken rule ("bad", empty scope + no fixtures). Whole-repo lint fails on
// "bad"; a run filtered to "good" must pass.
func lintTwoRuleRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: good\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: alpha}\n"+
			"  - id: bad\n    type: forbidden-pattern\n    scope: {include: ['nomatch/**']}\n    params: {pattern: beta}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "good", "fire-1", "f.txt"), "alpha want: good\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "good", "pass-1", "f.txt"), "clean\n")
	// A tracked file so good's '**' scope is non-empty (empty-scope check).
	mustWrite(t, filepath.Join(root, "src.txt"), "clean\n")
	return root
}

func TestLintRuleFilterRunsOnlyThatRule(t *testing.T) {
	root := lintTwoRuleRepo(t)
	// Sanity: the whole-repo lint fails because of "bad".
	if code, _, _ := runCLI(t, "lint", "-C", root); code != 1 {
		t.Fatalf("whole-repo lint exit = %d, want 1 (bad rule should fail)", code)
	}
	code, out, _ := runCLI(t, "lint", "-C", root, "--rule", "good")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (only the good rule should be checked)\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "bad") {
		t.Errorf("unfiltered rule should not be mentioned:\n%s", out)
	}
}

func TestLintRuleUnmatchedExits2(t *testing.T) {
	root := lintTwoRuleRepo(t)
	code, _, errOut := runCLI(t, "lint", "-C", root, "--rule", "no-such-rule")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (fail-closed on unmatched selector)\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "no-such-rule") {
		t.Errorf("stderr should name the unmatched rule:\n%s", errOut)
	}
}
