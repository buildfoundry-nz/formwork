package pattern

import (
	"errors"
	"fmt"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/rules/command"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// censusPattern runs a command detector as a forbidden-pattern. It does not
// implement rules.Coster and it does not embed *command: embedding would
// promote Cost() and --skip-escapes would drop the rule. CostOf therefore
// stays fast, which is what keeps the detector at pre-commit.
//
// CheckFile, SkipReason, Argv, and TriggerGlobs are forwarded. A when: gate
// whose CheckFile is not forwarded never sets sawTrigger, and FinalizeErr then
// returns no finding — a silent pass. Argv and TriggerGlobs are what
// formwork lint already asks, so a parent segment and a dead when: stay
// visible without a second lint check.
type censusPattern struct {
	inner rules.Checker
}

func censusDeclaresCost(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return censusDeclaresCost(n.Content[0])
	}
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "cost" {
			return true
		}
	}
	return false
}

func newCensus(p forbiddenParams) (rules.Checker, error) {
	if p.Multiline || p.Prefilter != "" || p.Window != 0 || p.Syntax != "" ||
		len(p.DeniedBy) > 0 || len(p.RequirePresent) > 0 || len(p.RequireAbsent) > 0 || len(p.NoneOf) > 0 {
		return nil, errors.New("forbidden-pattern: census cannot combine with multiline, prefilter, window, syntax, denied_by, require_present, require_absent, or none_of")
	}
	if censusDeclaresCost(p.Census.node) {
		return nil, errors.New("forbidden-pattern: census does not take cost; the rule stays fast so pre-commit runs the detector")
	}
	inner, err := command.NewChecker(p.Census.node)
	if err != nil {
		return nil, fmt.Errorf("forbidden-pattern: %w", err)
	}
	return &censusPattern{inner: inner}, nil
}

func (c *censusPattern) CheckFile(f *scan.File) ([]rules.Match, error) {
	return c.inner.CheckFile(f)
}

func (c *censusPattern) FinalizeErr(ctx rules.FinalizeContext) ([]rules.Match, error) {
	ef, ok := c.inner.(rules.ErrFinalizer)
	if !ok {
		return nil, errors.New("forbidden-pattern: census detector is not a finalizer")
	}
	return ef.FinalizeErr(ctx)
}

func (c *censusPattern) SkipReason() (string, bool) {
	return rules.SkipReasonOf(c.inner)
}

func (c *censusPattern) Argv() []string {
	type argvReader interface{ Argv() []string }
	a, ok := c.inner.(argvReader)
	if !ok {
		return nil
	}
	return a.Argv()
}

func (c *censusPattern) TriggerGlobs() []string {
	type triggerGated interface{ TriggerGlobs() []string }
	tg, ok := c.inner.(triggerGated)
	if !ok {
		return nil
	}
	return tg.TriggerGlobs()
}

// Recompile keeps the census wrap when fixturetest substitutes a compiled
// binary for `go run`. WithCompiledBinary on the inner command does not see
// this method, so the call does not recurse.
func (c *censusPattern) Recompile(binary string, progArgs []string, workDir string) (rules.Checker, bool) {
	inner, ok := command.WithCompiledBinary(c.inner, binary, progArgs, workDir)
	if !ok {
		return nil, false
	}
	return &censusPattern{inner: inner}, true
}
