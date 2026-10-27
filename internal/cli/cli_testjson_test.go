package cli_test

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
)

type jsonArm struct {
	Arm      string   `json:"arm"`
	Kind     string   `json:"kind"`
	Path     string   `json:"path"`
	OK       bool     `json:"ok"`
	Problems []string `json:"problems"`
}

type jsonRule struct {
	Rule   string    `json:"rule"`
	Status string    `json:"status"`
	Reason string    `json:"reason"`
	Arms   []jsonArm `json:"arms"`
}

type jsonDoc struct {
	Version int        `json:"version"`
	Rules   []jsonRule `json:"rules"`
	Summary struct {
		Rules       int `json:"rules"`
		RulesPassed int `json:"rules_passed"`
		Fixtures    int `json:"fixtures"`
	} `json:"summary"`
}

func decodeTestJSON(t *testing.T, out string) jsonDoc {
	t.Helper()
	var d jsonDoc
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if out[len(out)-1] != '\n' {
		t.Errorf("document lacks a trailing newline")
	}
	return d
}

func TestTestFormatJSONAllPass(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--format", "json", "--rule", "r1,r2")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	d := decodeTestJSON(t, out)
	if d.Version != 1 {
		t.Errorf("version = %d, want 1", d.Version)
	}
	if len(d.Rules) != 2 {
		t.Fatalf("got %d rules, want exactly the two selected: %s", len(d.Rules), out)
	}
	if d.Rules[0].Rule != "r1" || d.Rules[1].Rule != "r2" {
		t.Errorf("rules = %v, want r1, r2", d.Rules)
	}
	for _, r := range d.Rules {
		if r.Status != "ok" || len(r.Arms) != 2 {
			t.Errorf("rule %+v, want ok with 2 arms", r)
		}
	}
	if d.Summary.Rules != 2 || d.Summary.RulesPassed != 2 || d.Summary.Fixtures != 4 {
		t.Errorf("summary = %+v", d.Summary)
	}
}

func TestTestFormatJSONFailingRuleExits1(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--format", "json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	d := decodeTestJSON(t, out)
	ids := make([]string, 0, len(d.Rules))
	for _, r := range d.Rules {
		ids = append(ids, r.Rule)
	}
	if !sort.StringsAreSorted(ids) || len(ids) != 3 {
		t.Errorf("rules not sorted by id / wrong count: %v", ids)
	}
	r3 := d.Rules[2]
	if r3.Rule != "r3" || r3.Status != "fail" || r3.Arms[0].OK || len(r3.Arms[0].Problems) == 0 {
		t.Errorf("r3 = %+v, want fail with a problem on fire-1", r3)
	}
	if d.Summary.Rules != 3 || d.Summary.RulesPassed != 2 {
		t.Errorf("summary = %+v", d.Summary)
	}
	if d.Rules[0].Arms[0].Path != filepath.ToSlash(".formwork/fixtures/r1/fire-1") {
		t.Errorf("path = %q", d.Rules[0].Arms[0].Path)
	}
}

func TestTestFormatJSONExit2WritesNothingToStdout(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	code, out, _ := runCLI(t, "test", "-C", root, "--format", "json", "--rule", "nope")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout must be empty on exit 2, got:\n%s", out)
	}
}

func TestTestFormatUnknownExits2(t *testing.T) {
	root := threeRuleFixtureRepo(t)
	for _, f := range []string{"yaml", "github"} {
		code, out, _ := runCLI(t, "test", "-C", root, "--format", f)
		if code != 2 || out != "" {
			t.Errorf("--format %s: exit = %d stdout = %q, want 2 and empty", f, code, out)
		}
	}
}

// #49 S3: `vacuity` appears on pass arms only, never on fire arms, and a vacuous
// pass arm fails the run (exit 1) in JSON exactly as in human mode.
func TestTestFormatJSONVacuityOnPassArmsOnly(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "r.yaml"),
		"rules:\n  - id: v1\n    type: forbidden-pattern\n    scope: {include: ['src/**'], exclude: ['src/gen/**']}\n    params: {pattern: alpha}\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "v1", "fire-1", "src", "f.txt"), "alpha want: v1\n")
	mustWrite(t, filepath.Join(root, ".formwork", "fixtures", "v1", "pass-1", "src", "gen", "f.txt"), "clean\n")
	code, out, _ := runCLI(t, "test", "-C", root, "--format", "json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a vacuous pass arm\n%s", code, out)
	}
	var raw struct {
		Rules []struct {
			Arms []map[string]any `json:"arms"`
		} `json:"rules"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	arms := raw.Rules[0].Arms
	if len(arms) != 2 {
		t.Fatalf("arms = %v", arms)
	}
	if _, has := arms[0]["vacuity"]; has || arms[0]["arm"] != "fire-1" {
		t.Errorf("fire arm carries vacuity: %v", arms[0])
	}
	if arms[1]["vacuity"] != "vacuous" || arms[1]["vacuity_reason"] == "" || arms[1]["ok"] != false {
		t.Errorf("pass arm = %v, want vacuity=vacuous with a reason and ok=false", arms[1])
	}
}
