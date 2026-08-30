// selfskip_test.go — the self-skip census (#159). Separate from lint_test.go,
// which the 750-line vendor cap bounds; same package.
package meta_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// skipChecker is a Checker implementing rules.SkipReporter, so the census is
// tested against the INTERFACE rather than against `command` — the interface is
// the extension point, and a second implementor must not need this test edited.
type skipChecker struct {
	reason  string
	skipped bool
}

func (skipChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func (c skipChecker) SkipReason() (string, bool) { return c.reason, c.skipped }

// plainChecker implements no optional interface at all — the default every
// declarative rule type takes.
type plainChecker struct{}

func (plainChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

func rule(t *testing.T, id string, c rules.Checker) *config.Rule {
	t.Helper()
	r, err := config.New(id, "command", finding.SeverityError, "", []string{"**"}, nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSelfSkippedRulesNamesOnlyTheSkippers(t *testing.T) {
	rls := []*config.Rule{
		rule(t, "ran", skipChecker{}),
		rule(t, "skipped", skipChecker{reason: "skipped: when.paths_changed (db/**) matched no scanned file", skipped: true}),
		rule(t, "declarative", plainChecker{}),
	}

	got := meta.SelfSkippedRules(rls)
	if len(got) != 1 {
		t.Fatalf("got %d skips, want exactly the one rule that skipped: %+v", len(got), got)
	}
	if got[0].RuleID != "skipped" {
		t.Errorf("named the wrong rule: %+v", got[0])
	}
	if got[0].Reason != rls[1].Checker.(skipChecker).reason {
		t.Errorf("the checker's reason must travel verbatim, got %q", got[0].Reason)
	}
}

// Order is the order the rules were given, matching RulesMatchingNoFiles — the
// summary block is read top to bottom and a set-iteration order would make two
// identical runs render differently.
func TestSelfSkippedRulesPreservesRuleOrder(t *testing.T) {
	rls := []*config.Rule{
		rule(t, "c", skipChecker{reason: "skipped: c", skipped: true}),
		rule(t, "a", skipChecker{reason: "skipped: a", skipped: true}),
		rule(t, "b", skipChecker{reason: "skipped: b", skipped: true}),
	}
	got := meta.SelfSkippedRules(rls)
	if len(got) != 3 || got[0].RuleID != "c" || got[1].RuleID != "a" || got[2].RuleID != "b" {
		t.Fatalf("order not preserved: %+v", got)
	}
}

// A checker that skipped but said nothing is still named. Dropping it would put
// the census back where it started for exactly the rule that documented itself
// worst.
func TestSelfSkippedRuleWithNoReasonIsStillNamed(t *testing.T) {
	got := meta.SelfSkippedRules([]*config.Rule{rule(t, "mute", skipChecker{skipped: true})})
	if len(got) != 1 || got[0].RuleID != "mute" {
		t.Fatalf("a reasonless skip was dropped: %+v", got)
	}
}
