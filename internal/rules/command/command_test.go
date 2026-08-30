package command_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup("command")
	if !ok {
		t.Fatal("command type not registered")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	var node *yaml.Node
	if len(doc.Content) > 0 {
		node = doc.Content[0]
	}
	c, err := f(node)
	if err != nil {
		t.Fatalf("build command %q: %v", params, err)
	}
	return c
}

func finalize(t *testing.T, c rules.Checker) ([]rules.Match, error) {
	t.Helper()
	ef, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("command should implement ErrFinalizer")
	}
	return ef.FinalizeErr(rules.FinalizeContext{Root: t.TempDir()})
}

func TestCommandExitZeroPasses(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 0']")
	m, err := finalize(t, c)
	if err != nil || len(m) != 0 {
		t.Fatalf("expected pass, got matches=%v err=%v", m, err)
	}
}

func TestCommandUnexpectedExitFails(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 3']")
	m, err := finalize(t, c)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 1 || !strings.Contains(m[0].Message, "exited 3") {
		t.Fatalf("expected one finding naming exit 3, got %v", m)
	}
}

func TestCommandExpectedNonZeroExitPasses(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 2']\nexpect: {exit: 2}")
	m, err := finalize(t, c)
	if err != nil || len(m) != 0 {
		t.Fatalf("exit 2 with expect.exit 2 should pass, got matches=%v err=%v", m, err)
	}
}

func TestCommandOutputForbidFires(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'echo DANGER; exit 0']\nexpect: {output_forbid: DANGER}")
	m, err := finalize(t, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 || !strings.Contains(m[0].Message, "forbidden") {
		t.Fatalf("expected output_forbid finding, got %v", m)
	}
}

func TestCommandMissingBinaryIsEngineError(t *testing.T) {
	c := build(t, "cmd: [formwork-definitely-not-a-real-binary-xyz]")
	_, err := finalize(t, c)
	if err == nil {
		t.Fatal("a tool that cannot be executed must be an engine error, not a pass")
	}
}

func TestCommandHeavyCost(t *testing.T) {
	c := build(t, "cmd: [true]")
	if rules.CostOf(c) != rules.CostHeavy {
		t.Fatalf("command rules must be heavy, got %q", rules.CostOf(c))
	}
}

// Renamed from TestCommandProcessBoundIsArgv0: the predicate is no longer an
// argv0 comparison (#81 residual), so that name asserted something the code
// had stopped doing. The cases themselves still hold and are the base
// spellings; the wrapper spellings live in processbound_spelling_test.go.
func TestCommandProcessBoundBaseSpellings(t *testing.T) {
	cases := []struct {
		params string
		bound  bool
	}{
		{"cmd: [dart, run, packages/ui_audit_tool/bin/audit_scan.dart, .]", true},
		{"cmd: [flutter, analyze]", true},
		{"cmd: [go, run, scripts/dev/check-x.go, .]", false},
		{"cmd: [bash, scripts/dev/go-file-lists-reconcile.sh]", false},
		{"cmd: [true]", false},
	}
	for _, tc := range cases {
		c := build(t, tc.params)
		if rules.CostOf(c) != rules.CostHeavy {
			t.Fatalf("%s: Cost() = %q, want heavy (hooks / skip-escapes)", tc.params, rules.CostOf(c))
		}
		if got := rules.ProcessBoundOf(c); got != tc.bound {
			t.Fatalf("%s: ProcessBoundOf = %v, want %v", tc.params, got, tc.bound)
		}
	}
}

func TestCommandWhenPathsChangedSkipsWhenUntriggered(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 1']\nwhen: {paths_changed: ['db/migrations/**']}")
	// No CheckFile call matched the trigger → the command must NOT run
	// (so its exit 1 does not fire).
	m, err := finalize(t, c)
	if err != nil || len(m) != 0 {
		t.Fatalf("untriggered when-rule should skip, got matches=%v err=%v", m, err)
	}
}

func TestCommandWhenPathsChangedRunsWhenTriggered(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 1']\nwhen: {paths_changed: ['db/migrations/**']}")
	// A scanned file matched the trigger → the command runs and its exit 1 fires.
	if _, err := c.CheckFile(scan.NewMemFile("db/migrations/001.sql", []byte("x"))); err != nil {
		t.Fatal(err)
	}
	m, err := finalize(t, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("triggered when-rule should run and fire, got %v", m)
	}
}

// #159: the skip above is correct, and it was also invisible — the rule
// rendered `[id] OK` exactly like one whose tool ran and passed. These pin the
// disclosure at the checker, both directions.
func TestCommandSkippedRuleReportsWhyItSkipped(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 1']\nwhen: {paths_changed: ['db/migrations/**']}")
	if reason, skipped := rules.SkipReasonOf(c); skipped {
		t.Fatalf("nothing has run yet — a checker must not claim a skip it has not taken: %q", reason)
	}
	if _, err := finalize(t, c); err != nil {
		t.Fatal(err)
	}
	reason, skipped := rules.SkipReasonOf(c)
	if !skipped {
		t.Fatal("an untriggered when-rule skipped its tool and did not say so")
	}
	// The reason is rendered verbatim beside the rule id, so it must name the
	// gate, the globs that went unmatched, and the tool that did not run.
	for _, want := range []string{"when.paths_changed", "db/migrations/**", "did not run", "sh"} {
		if !strings.Contains(reason, want) {
			t.Errorf("skip reason %q missing %q", reason, want)
		}
	}
}

// The reason may only claim what the checker KNOWS. CheckFile is called for
// files passing the rule's own scope, so sawTrigger answers "did a file in this
// rule's scope match", never "did a scanned file match" — and a rule whose
// scope cannot reach its trigger paths at all (scope src/**, trigger db/**)
// would otherwise print "matched no scanned file" four lines under a summary
// saying the db/ file WAS scanned, sending the operator to inspect their
// changeset when the defect is in scope:.
func TestCommandSkipReasonDoesNotClaimNothingWasScanned(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 1']\nwhen: {paths_changed: ['db/**']}")
	// A file the ENGINE scanned and handed to this checker, which the trigger
	// does not match — the scope-restricted case in miniature.
	if _, err := c.CheckFile(scan.NewMemFile("src/a.go", []byte("x"))); err != nil {
		t.Fatal(err)
	}
	if _, err := finalize(t, c); err != nil {
		t.Fatal(err)
	}
	reason, skipped := rules.SkipReasonOf(c)
	if !skipped {
		t.Fatal("the rule skipped and did not say so")
	}
	if strings.Contains(reason, "no scanned file") {
		t.Errorf("the reason asserts something the checker cannot know: %q", reason)
	}
	if !strings.Contains(reason, "scope") {
		t.Errorf("the reason must name the scope the trigger was matched within: %q", reason)
	}
}

func TestCommandThatRanReportsNoSkip(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 0']\nwhen: {paths_changed: ['db/migrations/**']}")
	if _, err := c.CheckFile(scan.NewMemFile("db/migrations/001.sql", []byte("x"))); err != nil {
		t.Fatal(err)
	}
	if _, err := finalize(t, c); err != nil {
		t.Fatal(err)
	}
	if reason, skipped := rules.SkipReasonOf(c); skipped {
		t.Fatalf("a rule whose tool RAN was reported as skipped: %q", reason)
	}
}

// The flag describes the finalize that just happened, not the one before it. No
// caller re-finalizes an instance today, so this is latent rather than live —
// but a sticky true is the fail-open direction: the run whose tool EXECUTED
// would be the one reported as skipped.
func TestCommandSkipFlagDoesNotSurviveALaterRun(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 0']\nwhen: {paths_changed: ['db/**']}")
	if _, err := finalize(t, c); err != nil {
		t.Fatal(err)
	}
	if _, skipped := rules.SkipReasonOf(c); !skipped {
		t.Fatal("the first, untriggered finalize should have recorded a skip")
	}
	if _, err := c.CheckFile(scan.NewMemFile("db/001.sql", []byte("x"))); err != nil {
		t.Fatal(err)
	}
	if _, err := finalize(t, c); err != nil {
		t.Fatal(err)
	}
	if reason, skipped := rules.SkipReasonOf(c); skipped {
		t.Fatalf("a run whose tool executed still reported the earlier skip: %q", reason)
	}
}

// A rule with no `when:` at all has no gate to skip. Reporting one would fire
// the disclosure for every command rule in the corpus, which is the same as not
// having it.
func TestCommandWithoutWhenReportsNoSkip(t *testing.T) {
	c := build(t, "cmd: [sh, -c, 'exit 0']")
	if _, err := finalize(t, c); err != nil {
		t.Fatal(err)
	}
	if reason, skipped := rules.SkipReasonOf(c); skipped {
		t.Fatalf("a rule with no when: gate cannot skip: %q", reason)
	}
}

func TestCommandEmptyCmdRejected(t *testing.T) {
	if _, ok := rules.Lookup("command"); !ok {
		t.Fatal("not registered")
	}
	f, _ := rules.Lookup("command")
	if _, err := f(nil); err == nil {
		t.Fatal("empty/absent cmd must be rejected")
	}
}
