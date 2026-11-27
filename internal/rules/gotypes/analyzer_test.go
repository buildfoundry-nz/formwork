package gotypes

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func withPlan(t *testing.T, plan string) {
	t.Helper()
	prev := planFlag
	planFlag = plan
	t.Cleanup(func() { planFlag = prev })
}

func TestForbiddenTypeInPositionResolvesEveryPosition(t *testing.T) {
	withPlan(t, `{"analyzer":"forbidden-type-in-position","params":{"types":["any"],"positions":["field","param","result","var","elem","map","type-arg"]}}`)
	analysistest.Run(t, analysistest.TestData(), Analyzer, "positions")
}

func TestForbiddenTypeInPositionReportsOnlyTheListedPositions(t *testing.T) {
	withPlan(t, `{"analyzer":"forbidden-type-in-position","params":{"types":["any"],"positions":["param"]}}`)
	analysistest.Run(t, analysistest.TestData(), Analyzer, "onlyparams")
}

func TestAPlanTheVettoolCannotReadFailsThePackage(t *testing.T) {
	for name, plan := range map[string]string{
		"no plan":          ``,
		"not json":         `{`,
		"unknown analyzer": `{"analyzer":"nope","params":{}}`,
		"no positions":     `{"analyzer":"forbidden-type-in-position","params":{"types":["any"],"positions":[]}}`,
		"unknown position": `{"analyzer":"forbidden-type-in-position","params":{"types":["any"],"positions":["lhs"]}}`,
		"no types":         `{"analyzer":"forbidden-type-in-position","params":{"types":[],"positions":["param"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			withPlan(t, plan)
			if _, err := decodePlan(); err == nil {
				t.Fatal("plan accepted")
			} else if !strings.Contains(err.Error(), "formwork") {
				t.Fatalf("error %q does not name formwork", err)
			}
		})
	}
}
