package report_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/report"
)

func TestJSONCarriesCPUDurations(t *testing.T) {
	rls := []*config.Rule{{ID: "slow-rule"}, {ID: "fast-rule"}}
	var out bytes.Buffer
	report.JSON(&out, rls, nil, report.ScanSummary{}, report.Timing{
		Wall:     map[string]time.Duration{"slow-rule": 2100 * time.Millisecond, "fast-rule": 3 * time.Millisecond},
		CPU:      map[string]time.Duration{"slow-rule": 1500 * time.Millisecond, "fast-rule": 0},
		CPUTotal: 1900 * time.Millisecond,
	})
	var rep struct {
		Durations    map[string]int64 `json:"durations"`
		CPUDurations map[string]int64 `json:"cpu_durations"`
		CPUTotal     int64            `json:"cpu_total"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Durations["slow-rule"] != 2100 || rep.CPUDurations["slow-rule"] != 1500 {
		t.Fatalf("wall and CPU were conflated: %+v", rep)
	}
	if got, ok := rep.CPUDurations["fast-rule"]; !ok || got != 0 {
		t.Fatalf("a zero CPU entry must still be present: %+v", rep.CPUDurations)
	}
	if rep.CPUTotal != 1900 {
		t.Fatalf("cpu_total = %d, want 1900", rep.CPUTotal)
	}
}

func TestJSONWithoutCPUOmitsBothCPUKeys(t *testing.T) {
	var out bytes.Buffer
	report.JSON(&out, []*config.Rule{{ID: "rule"}}, nil, report.ScanSummary{}, report.Timing{
		Wall: map[string]time.Duration{"rule": 2 * time.Millisecond},
	})
	var rep map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if _, ok := rep["cpu_durations"]; ok {
		t.Fatalf("nil CPU emitted cpu_durations: %s", out.String())
	}
	if _, ok := rep["cpu_total"]; ok {
		t.Fatalf("nil CPU emitted cpu_total: %s", out.String())
	}
	if _, ok := rep["durations"]; !ok {
		t.Fatalf("wall-only timing lost durations: %s", out.String())
	}
}
