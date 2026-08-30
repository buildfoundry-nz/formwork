package finding_test

import (
	"reflect"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/finding"
)

func TestSortOrdersByRuleThenPathThenLineThenMessage(t *testing.T) {
	got := []finding.Finding{
		{RuleID: "b-rule", Path: "a.go", Line: 1, Message: "m"},
		{RuleID: "a-rule", Path: "z.go", Line: 9, Message: "m"},
		{RuleID: "a-rule", Path: "a.go", Line: 2, Message: "m"},
		{RuleID: "a-rule", Path: "a.go", Line: 1, Message: "n"},
		{RuleID: "a-rule", Path: "a.go", Line: 1, Message: "m"},
	}
	want := []finding.Finding{
		{RuleID: "a-rule", Path: "a.go", Line: 1, Message: "m"},
		{RuleID: "a-rule", Path: "a.go", Line: 1, Message: "n"},
		{RuleID: "a-rule", Path: "a.go", Line: 2, Message: "m"},
		{RuleID: "a-rule", Path: "z.go", Line: 9, Message: "m"},
		{RuleID: "b-rule", Path: "a.go", Line: 1, Message: "m"},
	}
	finding.Sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Sort order wrong:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestSeverityConstants(t *testing.T) {
	if finding.SeverityError != "error" || finding.SeverityWarn != "warn" {
		t.Fatalf("severity constants changed: %q %q", finding.SeverityError, finding.SeverityWarn)
	}
}
