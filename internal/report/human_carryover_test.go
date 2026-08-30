package report_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/report"
)

func TestHumanZeroRules(t *testing.T) {
	var sb strings.Builder
	report.Human(&sb, nil, nil, report.ScanSummary{})
	want := "scan: 0 file(s) scanned\nformwork: 0/0 rules passed, 0 finding(s)\n"
	if sb.String() != want {
		t.Fatalf("got %q, want %q", sb.String(), want)
	}
}
