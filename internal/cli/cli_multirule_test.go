package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// threeRuleFixtureRepo is twoRuleFixtureRepo plus a third rule whose fire
// fixture carries no want marker, so running it FAILS. A selection that
// excludes it must still exit 0; one that reaches it cannot.
func threeRuleFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n"+
			"  - id: r1\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: alpha}\n"+
			"  - id: r2\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: beta}\n"+
			"  - id: r3\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: gamma}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "r1", "fire-1", "f.txt"), "alpha want: r1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "r1", "pass-1", "f.txt"), "clean\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "r2", "fire-1", "f.txt"), "beta want: r2\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "r2", "pass-1", "f.txt"), "clean\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "r3", "fire-1", "f.txt"), "gamma\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "r3", "pass-1", "f.txt"), "clean\n")
	return root
}

func assertRan(t *testing.T, code int, out string, wantRules ...string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("exit = %d, want 0\noutput:\n%s", code, out)
	}
	for _, id := range wantRules {
		if !strings.Contains(out, "["+id+"] OK") {
			t.Errorf("output missing %s verdict:\n%s", id, out)
		}
	}
	if strings.Contains(out, "r3") {
		t.Errorf("unselected rule r3 ran:\n%s", out)
	}
	if !strings.Contains(out, "2/2 rules passed") {
		t.Errorf("summary should count exactly the two selected rules:\n%s", out)
	}
}

func TestTestRuleRepeatedFlagRunsEachSelectedRule(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--rule", "r1", "--rule", "r2")
	assertRan(t, code, out, "r1", "r2")
}

func TestTestRuleCommaSeparatedRunsEachSelectedRule(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--rule", " r1 , r2,")
	assertRan(t, code, out, "r1", "r2")
}

func TestTestRuleUnknownAmongKnownExits2RunningNothing(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, errOut := runCLI(t, "test", "-C", root, "--rule", "r1", "--rule", "nope")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, `no rule matches --rule "nope"`) {
		t.Errorf("stderr should name the unknown id:\n%s", errOut)
	}
	if strings.Contains(errOut, `"r1"`) {
		t.Errorf("known id reported as unknown:\n%s", errOut)
	}
	if strings.Contains(out, "r1") {
		t.Errorf("a rule ran despite the unknown id:\n%s", out)
	}
}

func TestTestRuleEachUnknownIDReportedInOrder(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, _, errOut := runCLI(t, "test", "-C", root, "--rule", "zz,r1,aa")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	zz, aa := strings.Index(errOut, `"zz"`), strings.Index(errOut, `"aa"`)
	if zz < 0 || aa < 0 || zz > aa {
		t.Errorf("want zz then aa, each reported:\n%s", errOut)
	}
}

func TestTestRuleDuplicatesRunOnce(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--rule", "r1,r1")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if n := strings.Count(out, "[r1] OK"); n != 1 {
		t.Errorf("r1 ran %d times, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "1/1 rules passed") {
		t.Errorf("summary should count r1 once:\n%s", out)
	}
}

func TestTestRuleDuplicateUnknownReportedOnce(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, _, errOut := runCLI(t, "test", "-C", root, "--rule", "nope,nope", "--rule", "nope")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if n := strings.Count(errOut, `no rule matches --rule "nope"`); n != 1 {
		t.Errorf("unknown id reported %d times, want 1:\n%s", n, errOut)
	}
}
