package fixturetest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
)

const resultsConfig = `rules:
  - id: a-fruit-free
    type: forbidden-pattern
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'banana'
  - id: b-has-anchor
    type: required-pattern
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'anchor'
      mode: exists
  - id: c-no-fixtures
    type: forbidden-pattern
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'zzz'
`

func resultsRepo(t *testing.T) (*config.Config, string) {
	t.Helper()
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  resultsConfig,
		// a-fruit-free: one fire, one pass — passes.
		".formwork/fixtures/a-fruit-free/pass-1/f.txt": "all clean\n",
		".formwork/fixtures/a-fruit-free/fire-1/f.txt": "a banana here want: a-fruit-free\n",
		// b-has-anchor: the fire arm declares no expectation — fails.
		".formwork/fixtures/b-has-anchor/fire-1/a.md": "nothing here\n",
		".formwork/fixtures/b-has-anchor/pass-1/a.md": "the anchor\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, root
}

func TestResultsStructuredPerArm(t *testing.T) {
	cfg, root := resultsRepo(t)
	got, err := fixturetest.Results(cfg, fullIDs(cfg), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rule results, want 3: %+v", len(got), got)
	}

	ok := got[0]
	if ok.Rule != "a-fruit-free" || ok.Status != fixturetest.StatusOK || ok.Reason != "" {
		t.Errorf("passing rule = %+v", ok)
	}
	wantArms := []fixturetest.ArmResult{
		{Arm: "fire-1", Kind: "fire", Path: ".formwork/fixtures/a-fruit-free/fire-1", OK: true},
		{Arm: "pass-1", Kind: "pass", Path: ".formwork/fixtures/a-fruit-free/pass-1", OK: true},
	}
	if !reflect.DeepEqual(ok.Arms, wantArms) {
		t.Errorf("passing rule arms (want sorted fire-1, pass-1) =\n%+v\nwant\n%+v", ok.Arms, wantArms)
	}

	bad := got[1]
	if bad.Rule != "b-has-anchor" || bad.Status != fixturetest.StatusFail {
		t.Fatalf("failing rule = %+v", bad)
	}
	if len(bad.Arms) != 2 || bad.Arms[0].Arm != "fire-1" || bad.Arms[1].Arm != "pass-1" {
		t.Fatalf("failing rule arms = %+v", bad.Arms)
	}
	if bad.Arms[0].OK || len(bad.Arms[0].Problems) == 0 || bad.Arms[0].Kind != "fire" {
		t.Errorf("failing fire arm = %+v, want OK=false with problems", bad.Arms[0])
	}
	if !strings.HasPrefix(bad.Arms[0].Problems[0], "fire-1: ") {
		t.Errorf("problem %q should carry the arm prefix the human output prints", bad.Arms[0].Problems[0])
	}
	if !bad.Arms[1].OK || len(bad.Arms[1].Problems) != 0 {
		t.Errorf("passing pass arm = %+v", bad.Arms[1])
	}

	skip := got[2]
	if skip.Rule != "c-no-fixtures" || skip.Status != fixturetest.StatusSkip || skip.Reason == "" {
		t.Errorf("no-fixture rule = %+v, want skip with a reason", skip)
	}
	if skip.Arms == nil || len(skip.Arms) != 0 {
		t.Errorf("skip arms = %#v, want empty non-nil", skip.Arms)
	}
}

// Run renders from Results: every FAIL/OK/SKIP line must agree with the
// structured status for the same rule.
func TestRunTextAgreesWithResults(t *testing.T) {
	cfg, root := resultsRepo(t)
	results, err := fixturetest.Results(cfg, fullIDs(cfg), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
	want := map[string]string{
		fixturetest.StatusOK:   "[a-fruit-free] OK — 2 fixture(s)",
		fixturetest.StatusFail: "[b-has-anchor] FAIL — 1 problem(s)",
		fixturetest.StatusSkip: "[c-no-fixtures] SKIP — no fixtures",
	}
	for _, rr := range results {
		line := want[rr.Status]
		if !strings.Contains(sb.String(), line) {
			t.Errorf("status %s for %s: text lacks %q:\n%s", rr.Status, rr.Rule, line, sb.String())
		}
	}
	if !strings.Contains(sb.String(), "formwork test: 1/2 rules passed, 4 fixture(s) run, 1 rule(s) skipped") {
		t.Errorf("summary line wrong:\n%s", sb.String())
	}
}
