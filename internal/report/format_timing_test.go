package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/report"
)

// TestJSONCarriesDurations pins the additive contract: a caller that hands
// RunTimed's Timing map gets a "durations" object keyed by rule id with whole
// milliseconds; a caller that passes nil gets bytes with NO durations key at
// all (omitempty — old consumers keep their exact shape, the field is a new
// nascent contract like cure: was, not a breaking one).
func TestJSONCarriesDurations(t *testing.T) {
	rules := []*config.Rule{{ID: "slow-rule"}, {ID: "fast-rule"}}
	var with bytes.Buffer
	report.JSON(&with, rules, nil, report.ScanSummary{}, map[string]time.Duration{
		"slow-rule": 1500 * time.Millisecond,
		"fast-rule": 2 * time.Millisecond,
	})
	var rep struct {
		Durations map[string]int64 `json:"durations"`
	}
	if err := json.Unmarshal(with.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Durations["slow-rule"] != 1500 {
		t.Fatalf("slow-rule ms = %d, want 1500: %s", rep.Durations["slow-rule"], with.String())
	}
	if rep.Durations["fast-rule"] != 2 {
		t.Fatalf("fast-rule ms = %d, want 2", rep.Durations["fast-rule"])
	}

	var without bytes.Buffer
	report.JSON(&without, rules, nil, report.ScanSummary{}, nil)
	if strings.Contains(without.String(), `"durations"`) {
		t.Fatalf("nil timing must omit the key entirely: %s", without.String())
	}
}
