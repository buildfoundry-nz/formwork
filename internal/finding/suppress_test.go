package finding_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/finding"
)

func TestUnsuppressedFiltersAndPreservesOrder(t *testing.T) {
	fs := []finding.Finding{
		{RuleID: "a", Path: "1.go"},
		{RuleID: "a", Path: "2.go", Suppressed: true, SuppressedBy: "marker"},
		{RuleID: "b", Path: "3.go"},
	}
	got := finding.Unsuppressed(fs)
	if len(got) != 2 || got[0].Path != "1.go" || got[1].Path != "3.go" {
		t.Fatalf("Unsuppressed = %+v", got)
	}
	if got := finding.Unsuppressed(nil); len(got) != 0 {
		t.Fatalf("Unsuppressed(nil) = %+v", got)
	}
}
