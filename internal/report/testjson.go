package report

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
)

// TestJSONVersion is the schema version of the `formwork test --format json`
// document; it bumps on any breaking change to that shape.
const TestJSONVersion = 1

type testJSONSummary struct {
	Rules       int `json:"rules"`
	RulesPassed int `json:"rules_passed"`
	Fixtures    int `json:"fixtures"`
}

type testJSONReport struct {
	Version int                      `json:"version"`
	Rules   []fixturetest.RuleResult `json:"rules"`
	Summary testJSONSummary          `json:"summary"`
}

// TestJSON writes the fixture results as one JSON object with a trailing
// newline. Rules are ordered by id here, whatever order the caller holds them
// in, and the summary counts follow the human summary line: skipped rules are
// in neither `rules` nor `rules_passed`.
func TestJSON(w io.Writer, results []fixturetest.RuleResult) error {
	rs := make([]fixturetest.RuleResult, len(results))
	copy(rs, results)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Rule < rs[j].Rule })
	var sum testJSONSummary
	for i := range rs {
		if rs[i].Arms == nil {
			rs[i].Arms = []fixturetest.ArmResult{}
		}
		switch rs[i].Status {
		case fixturetest.StatusSkip:
			continue
		case fixturetest.StatusOK:
			sum.RulesPassed++
		}
		sum.Rules++
		sum.Fixtures += len(rs[i].Arms)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(testJSONReport{Version: TestJSONVersion, Rules: rs, Summary: sum})
}
