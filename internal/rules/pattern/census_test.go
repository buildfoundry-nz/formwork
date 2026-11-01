package pattern_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/rules/command"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func censusFinalize(t *testing.T, c rules.Checker) ([]rules.Match, error) {
	t.Helper()
	ef, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("census mode must finalize")
	}
	return ef.FinalizeErr(rules.FinalizeContext{Root: t.TempDir()})
}

func TestCensusExitZeroStaysSilent(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "census:\n  cmd: [sh, -c, 'exit 0']\n")
	if _, isCost := c.(rules.Coster); isCost {
		t.Fatal("census wrapper must not declare a cost; CostOf defaults to fast so --skip-escapes keeps the detector")
	}
	if rules.CostOf(c) != rules.CostFast {
		t.Fatalf("census cost = %q, want fast", rules.CostOf(c))
	}
	m, err := censusFinalize(t, c)
	if err != nil || len(m) != 0 {
		t.Fatalf("exit 0 must stay silent, matches=%v err=%v", m, err)
	}
}

func TestCensusNonZeroExitIsAFinding(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "census:\n  cmd: [sh, -c, 'echo hand-built; exit 1']\n")
	m, err := censusFinalize(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || !strings.Contains(m[0].Message, "exited 1") || !strings.Contains(m[0].Message, "hand-built") {
		t.Fatalf("non-zero census must be a finding carrying the detector output, got %v", m)
	}
}

func TestCensusRejectsASecondMode(t *testing.T) {
	for _, params := range []string{
		"pattern: x\ncensus:\n  cmd: [true]\n",
		"all_of: [a, b]\ncensus:\n  cmd: [true]\n",
		"census:\n  cmd: [true]\nmultiline: true\n",
		"census:\n  cmd: [true]\nprefilter: x\n",
		"census:\n  cmd: [true]\nwindow: 2\n",
		"census:\n  cmd: [true]\ndenied_by: [not]\n",
		"census:\n  cmd: [true]\nrequire_present: [x]\n",
		"census:\n  cmd: [true]\n  cost: range\n",
		"{}\n",
	} {
		factory, ok := rules.Lookup("forbidden-pattern")
		if !ok {
			t.Fatal("forbidden-pattern not registered")
		}
		_, err := factory(paramsNode(t, params))
		if err == nil {
			t.Fatalf("params must be refused:\n%s", params)
		}
	}
}

func TestCensusForwardsTheWhenGate(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "census:\n  cmd: [sh, -c, 'exit 3']\n  when:\n    paths_changed: ['**/*.go']\n")
	m, err := censusFinalize(t, c)
	if err != nil || len(m) != 0 {
		t.Fatalf("an unarmed when: gate must not run the detector, matches=%v err=%v", m, err)
	}
	reason, skipped := rules.SkipReasonOf(c)
	if !skipped || !strings.Contains(reason, "when.paths_changed") {
		t.Fatalf("skip must be visible, reason=%q skipped=%v", reason, skipped)
	}

	armed := mustChecker(t, "forbidden-pattern", "census:\n  cmd: [sh, -c, 'exit 3']\n  when:\n    paths_changed: ['**/*.go']\n")
	if _, err := armed.CheckFile(scan.NewMemFile("a.go", []byte("package a\n"))); err != nil {
		t.Fatal(err)
	}
	m, err = censusFinalize(t, armed)
	if err != nil || len(m) != 1 || !strings.Contains(m[0].Message, "exited 3") {
		t.Fatalf("a file matching when: must run the detector, matches=%v err=%v", m, err)
	}
}

func TestCensusForwardsArgvForTheParentSegmentCheck(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "census:\n  cmd: [sh, -c, 'echo', '../outside']\n")
	type argvReader interface{ Argv() []string }
	a, ok := c.(argvReader)
	if !ok {
		t.Fatal("census must expose Argv so lint sees a parent segment")
	}
	arg, bad := command.ParentSegmentArg(a.Argv())
	if !bad || !strings.Contains(arg, "..") {
		t.Fatalf("parent segment not visible, arg=%q bad=%v argv=%v", arg, bad, a.Argv())
	}
}

func TestCensusRecompileKeepsTheFastWrap(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "census:\n  cmd: [sh, -c, 'exit 3']\n")
	rewritten, ok := command.WithCompiledBinary(c, "sh", []string{"-c", "exit 0"}, "")
	if !ok {
		t.Fatal("compile-once must rewrite a census detector")
	}
	if rules.CostOf(rewritten) != rules.CostFast {
		t.Fatalf("rewritten census cost = %q, want fast", rules.CostOf(rewritten))
	}
	m, err := censusFinalize(t, rewritten)
	if err != nil || len(m) != 0 {
		t.Fatalf("rewritten argv must be what runs, matches=%v err=%v", m, err)
	}
}
