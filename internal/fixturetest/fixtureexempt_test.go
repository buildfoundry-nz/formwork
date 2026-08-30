// fixtureexempt_test.go — #336, the THIRD consumer of fixture_exempt.
//
// `formwork lint` (internal/meta/fixturecoverage.go) and the escape-hatch
// census (internal/meta/census.go) both read the field through the same
// predicate: in force only when it declares something AND the rule it is on is
// heavy. `formwork test` read it as a bare `!= ""` on every rule, so the two
// commands disagreed about the same rule in the same repository — one SKIPped
// it at exit 0 while the other FAILed it for the fixtures the exemption did
// not buy.
package fixturetest_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// heavyChecker is a CostHeavy checker that finds nothing. fixture_exempt keys
// on the rule's COST, not on its `type:` string (internal/meta:isExternalTool
// is `r.Cost() == rules.CostHeavy`), so the cost class is the whole of what
// this has to be.
type heavyChecker struct{ nopChecker }

func (heavyChecker) Cost() rules.Cost { return rules.CostHeavy }

// TestContentFreeFixtureExemptIsNotADeclaredExemption pins the trim half.
//
// The rule is built with config.New and the exported field rather than loaded
// from YAML, and it has to stay that way: the loader refuses a content-free
// `fixture_exempt` outright now (internal/config/fixtureexempt.go), so the
// state is no longer reachable through a rule file. It stays reachable through
// the public seam — config.Config.Rules is a plain exported []*config.Rule, so
// a caller of config.New/fixturetest.Run constructs one directly, the route
// TestRunErrorsWhenRuleCannotBeRefreshed already takes — and Run's verdict
// cannot depend on the caller having come through the loader.
func TestContentFreeFixtureExemptIsNotADeclaredExemption(t *testing.T) {
	root := writeRepo(t, nil)

	r, err := config.New("heavy-blank", "command", finding.SeverityError, "",
		[]string{"**"}, nil, nil, heavyChecker{})
	if err != nil {
		t.Fatal(err)
	}
	r.FixtureExempt = "   "
	cfg := &config.Config{Version: 1, Rules: []*config.Rule{r}}

	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Fatalf("failed=%d, want 0 — a skipped rule is not a failing one\n%s", failed, sb.String())
	}
	out := sb.String()
	if strings.Contains(out, "declared fixture-exempt") {
		t.Fatalf("three spaces bought the declared-exemption line, which then renders with "+
			"nothing after the colon:\n%s", out)
	}
	if !strings.Contains(out, "[heavy-blank] SKIP — no fixtures (formwork lint reports coverage)") {
		t.Fatalf("a heavy rule whose declaration declares nothing is undeclared, and must be "+
			"skipped as such:\n%s", out)
	}
}

// TestFixtureExemptOnAFastRuleIsNotADeclaredExemption pins the heavy half.
//
// fixture_exempt governs heavy rules only (docs/reference.md, `fixture_exempt`
// section). fixture-coverage judges a fast rule on its fire/pass fixtures
// whatever it declares — so a fast rule carrying a real, honest reason and no
// fixtures was SKIPped by `formwork test` at exit 0 and FAILed by
// `formwork lint` one command away.
func TestFixtureExemptOnAFastRuleIsNotADeclaredExemption(t *testing.T) {
	failed, out := run(t, map[string]string{
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: fast-exempt\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.ts']}\n" +
			"    fixture_exempt: \"I just don't want fixtures\"\n" +
			"    params: {pattern: WIDGET}\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d, want 0 — a skipped rule is not a failing one\n%s", failed, out)
	}
	if strings.Contains(out, "declared fixture-exempt") {
		t.Fatalf("a fast rule's fixture_exempt bought a skip it does not govern, while "+
			"fixture-coverage demands that same rule's fire and pass fixtures:\n%s", out)
	}
	if !strings.Contains(out, "[fast-exempt] SKIP — no fixtures (formwork lint reports coverage)") {
		t.Fatalf("a fast rule with no fixtures is undeclared to `test`, the verdict "+
			"`formwork lint` reports for it:\n%s", out)
	}
}
