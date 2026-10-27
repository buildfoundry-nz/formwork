package cli

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// invariantChecker is a whole-tree invariant whose ScopeBound answer the test
// chooses. CheckFile is never called: the gate only reads declarations.
type invariantChecker struct{ bound bool }

// perFileChecker is neither an invariant nor SubjectScoped: an ordinary
// per-file rule, which already sees only the changed files.
type perFileChecker struct{}

func (perFileChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func (invariantChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (invariantChecker) WholeTreeInvariant() bool                    { return true }
func (c invariantChecker) ScopeBound() bool                          { return c.bound }

func gateRule(t *testing.T, id string, bound bool) *config.Rule {
	t.Helper()
	r, err := config.New(id, "fake", finding.SeverityError, "", []string{"src/**/*.go"}, nil, invariantChecker{bound: bound})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ranIDs(rs []*config.Rule) []string {
	ids := make([]string, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	return ids
}

// A checker that has not declared its inputs are its scope may read anything,
// so no changeset can be shown not to reach it.
func TestGateRunsAnInvariantThatIsNotScopeBound(t *testing.T) {
	run, skipped := gateOnChangeset([]*config.Rule{gateRule(t, "unbound", false)}, []string{"docs/readme.md"})
	if len(run) != 1 || len(skipped) != 0 {
		t.Fatalf("an invariant that is not ScopeBound must run on any changeset: ran %v, skipped %+v", ranIDs(run), skipped)
	}
}

// The skip is per rule: an unreached rule beside a reached one is skipped, and
// the reached one still runs.
func TestGateSkipsOnlyTheUnreachedInvariant(t *testing.T) {
	reached := gateRule(t, "reached", true)
	unreached, err := config.New("unreached", "fake", finding.SeverityError, "", []string{"other/**/*.go"}, nil, invariantChecker{bound: true})
	if err != nil {
		t.Fatal(err)
	}
	run, skipped := gateOnChangeset([]*config.Rule{reached, unreached}, []string{"src/a/b.go"})
	if ids := ranIDs(run); len(ids) != 1 || ids[0] != "reached" {
		t.Fatalf("only the reached invariant should run, ran %v", ids)
	}
	if len(skipped) != 1 || skipped[0].RuleID != "unreached" {
		t.Fatalf("the unreached invariant should be named as skipped, got %+v", skipped)
	}
}

// Only the two declared classes are gated. An ordinary per-file rule is handed
// the changed files and judges those; removing it would hide nothing it could
// see, but the gate has no business deciding that for it.
func TestGateLeavesAnOrdinaryPerFileRuleAlone(t *testing.T) {
	r, err := config.New("plain", "fake", finding.SeverityError, "", []string{"src/**/*.go"}, nil, perFileChecker{})
	if err != nil {
		t.Fatal(err)
	}
	run, skipped := gateOnChangeset([]*config.Rule{r}, []string{"docs/readme.md"})
	if len(run) != 1 || len(skipped) != 0 {
		t.Fatalf("a rule that declares neither class must not be gated: ran %v, skipped %+v", ranIDs(run), skipped)
	}
}
