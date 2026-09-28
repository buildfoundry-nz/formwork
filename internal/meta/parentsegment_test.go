package meta

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	"gopkg.in/yaml.v3"
)

// ruleWithArgv builds a loaded command rule for the check to judge.
func ruleWithArgv(t *testing.T, id, params string) *config.Rule {
	t.Helper()
	f, ok := rules.Lookup("command")
	if !ok {
		t.Fatal("command type not registered")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	c, err := f(doc.Content[0])
	if err != nil {
		t.Fatalf("build %q: %v", params, err)
	}
	return &config.Rule{ID: id, Type: "command", Checker: c}
}

// The check reports a parent segment and names the rule, so an author reading
// lint's output knows which argv to migrate.
func TestCommandArgvProblems_ReportsAParentSegment(t *testing.T) {
	rls := []*config.Rule{
		ruleWithArgv(t, "climbs-out", "cmd: [go, -C, scripts/dev/x, run, ., --root, ../../..]"),
		ruleWithArgv(t, "names-its-tree", "cmd: [go, -C, '{{repo}}/scripts/dev/x', run, ., --root, '{{root}}']"),
	}
	got := commandArgvProblems(rls)
	if len(got) != 1 {
		t.Fatalf("want one problem, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "climbs-out") {
		t.Errorf("the problem does not name the rule: %s", got[0])
	}
	if !strings.Contains(got[0], "{{root}}") {
		t.Errorf("the problem does not carry the migration: %s", got[0])
	}
}

// The check enters lint's denominator only where it has a subject.
func TestAnyCommandArgv_NeedsACommandRule(t *testing.T) {
	if anyCommandArgv(nil) {
		t.Error("an empty corpus has no command rule to judge")
	}
	if !anyCommandArgv([]*config.Rule{ruleWithArgv(t, "r", "cmd: [sh, -c, 'true']")}) {
		t.Error("a corpus with a command rule has a subject")
	}
}
