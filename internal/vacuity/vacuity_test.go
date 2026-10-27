package vacuity_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vacuity"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// loadRule loads the single rule in ruleYAML through the real config loader,
// so the rule carries the factory and params node a production rule does.
func loadRule(t *testing.T, ruleYAML string) *config.Rule {
	t.Helper()
	root := t.TempDir()
	write(t, root, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  "rules:\n" + ruleYAML,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("want one rule, got %d", len(cfg.Rules))
	}
	return cfg.Rules[0]
}

func walk(t *testing.T, files map[string]string) *scan.FileSet {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, files)
	fs, err := scan.Walk(dir)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func judge(t *testing.T, ruleYAML string, files map[string]string) vacuity.Verdict {
	t.Helper()
	v, err := vacuity.Judge(loadRule(t, ruleYAML), walk(t, files), nil)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func wantVerdict(t *testing.T, v vacuity.Verdict, kind, code string) {
	t.Helper()
	if v.Kind != kind || v.Code != code {
		t.Fatalf("verdict = %+v, want kind %q code %q", v, kind, code)
	}
	if kind != vacuity.KindJudged && v.Reason == "" {
		t.Fatalf("verdict %+v carries no reason", v)
	}
}

const banScoped = `
  - id: inert-pass
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/generated/**']
    params: { pattern: 'BANNED' }
`

// --- PASS-FIXTURE-VACUOUS (ported from cmd/formwork-vacuity-census/passvacuous_test.go) ---

func TestVacuousWhenNoFileReachedAndNothingJudged(t *testing.T) {
	v := judge(t, banScoped, map[string]string{
		"src/generated/g.go": "package generated\n\nvar y = fine\n",
	})
	wantVerdict(t, v, vacuity.KindVacuous, vacuity.CodeVacuous)
}

func TestNotVacuousWhenParkingARealViolationAtAnExcludedPath(t *testing.T) {
	v := judge(t, banScoped, map[string]string{
		"src/generated/g.go": "package generated\n\nvar y = BANNED\n",
	})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestNotVacuousWhenOutOfScopeFileLacksARequiredPattern(t *testing.T) {
	v := judge(t, `
  - id: suffix-anchored-requirement
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*_card.go']
    params: { pattern: 'CardFrame\(', mode: every-file }
`, map[string]string{"src/b_row.go": "package b\n\nvar w = handRolled()\n"})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestNotVacuousWhenTheRuleReachesAFile(t *testing.T) {
	v := judge(t, banScoped, map[string]string{"src/a.go": "package a\n\nvar y = fine\n"})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestVacuousHonoursDeclaredInert(t *testing.T) {
	r := loadRule(t, banScoped)
	fs := walk(t, map[string]string{"src/generated/g.go": "package generated\n"})
	got, err := vacuity.Vacuous(r, fs, func(*scan.FileSet) bool { return true })
	if err != nil || got {
		t.Fatalf("Vacuous with a declared-inert fixture = %v, %v; want false, nil", got, err)
	}
	got, err = vacuity.Vacuous(r, fs, func(*scan.FileSet) bool { return false })
	if err != nil || !got {
		t.Fatalf("Vacuous with nothing declared = %v, %v; want true, nil", got, err)
	}
}

// --- PASS-FIXTURE-BLIND (ported from the downstream census's passblind_test.go) ---

const confinement = `
  - id: %s
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: '^(marshalJob|proto\.Marshal)$'
      allowed_func: '^encodePayload$'
`

func TestBlindPassFixtureGates(t *testing.T) {
	v := judge(t, fmt.Sprintf(confinement, "blind-pass-fixture"), map[string]string{
		"src/a.go": "package a\n\nfunc encodePayload() {\n\tjson.Marshal(m)\n}\n",
	})
	wantVerdict(t, v, vacuity.KindBlind, vacuity.CodeBlind)
}

func TestPassFixtureCallingTheSymbolFromTheAllowedFuncIsNotBlind(t *testing.T) {
	v := judge(t, fmt.Sprintf(confinement, "real-pass-fixture"), map[string]string{
		"src/a.go": "package a\n\nfunc encodePayload() {\n\tproto.Marshal(m)\n}\n",
	})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestTotalBanPassFixtureWithoutTheSymbolIsNotBlind(t *testing.T) {
	v := judge(t, `
  - id: total-ban-pass-fixture
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: '^countWallHeightMetrics$'
      allowed_func: '^$'
`, map[string]string{
		"src/a.go": "package a\n\nfunc gather() {\n\tcountWallHeightMetricsForPages()\n}\n",
	})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestOutOfScopePassFixtureIsNotBlind(t *testing.T) {
	v := judge(t, `
  - id: scope-test-pass-fixture
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/legacy/**']
    params:
      symbol: '^marshalJob$'
      allowed_func: '^encodePayload$'
`, map[string]string{
		"src/legacy/old.go": "package legacy\n\nfunc rogue() {\n\tmarshalJob()\n}\n",
	})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestPassFixtureDemonstratingTheFuncsFilterIsNotBlind(t *testing.T) {
	v := judge(t, `
  - id: funcs-filter-pass-fixture
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^authorize$'
      sink: '^deleteRow$'
      funcs: '^handle'
`, map[string]string{
		"src/a.go": "package a\n\nfunc migrate() {\n\tdeleteRow()\n}\n",
	})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

func TestBlindPassFixtureGatesGuardPrecedes(t *testing.T) {
	v := judge(t, `
  - id: blind-pass-fixture-sink
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^authorize$'
      sink: '^deleteRow$'
`, map[string]string{
		"src/a.go": "package a\n\nfunc handle() {\n\tauthorize()\n\tsave()\n}\n",
	})
	wantVerdict(t, v, vacuity.KindBlind, vacuity.CodeBlind)
}

// The funcs-probe arm the port adds on top of the first probe: a rule with
// `funcs` set and NO sink anywhere stays blind after the second probe too.
func TestGuardPrecedesWithFuncsAndNoSinkIsStillBlind(t *testing.T) {
	v := judge(t, `
  - id: funcs-no-sink
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^authorize$'
      sink: '^deleteRow$'
      funcs: '^handle'
`, map[string]string{
		"src/a.go": "package a\n\nfunc handleX() {\n\tauthorize()\n\tsave()\n}\n",
	})
	wantVerdict(t, v, vacuity.KindBlind, vacuity.CodeBlind)
}

// Known quirk, kept on purpose: blind's anchor probe counts SUPPRESSED findings,
// so a sink that is present but marker-suppressed is a hit and the fixture is
// judged — whereas the vacuous probe counts only unsuppressed ones.
func TestBlindCountsSuppressedAnchorHits(t *testing.T) {
	v := judge(t, fmt.Sprintf(confinement, "suppressed-anchor"), map[string]string{
		"src/a.go": "package a\n\nfunc encodePayload() {\n\t// formwork:allow suppressed-anchor -- test\n\tjson.Marshal(m)\n\tmarshalJob()\n}\n",
	})
	wantVerdict(t, v, vacuity.KindJudged, "")
}

// --- not-judged and errors ---

func TestEscapeRuleIsNotJudged(t *testing.T) {
	v := judge(t, `
  - id: runs-a-tool
    type: command
    severity: error
    scope:
      include: ['**']
    params:
      cmd: ['true']
`, map[string]string{"x.txt": "x\n"})
	wantVerdict(t, v, vacuity.KindNotJudged, "")
}

// A rule built without params has an unreadable (empty) anchor pattern: a config
// defect, so Judge errors (exit 2) rather than calling the fixture judged.
func TestEmptyAnchorPatternIsAnError(t *testing.T) {
	r, err := config.New("empty-anchor", "go/guard-precedes-call", finding.SeverityError, "", []string{"**"}, nil, nil, stubChecker{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vacuity.Judge(r, walk(t, map[string]string{"a.go": "package a\n"}), nil); err == nil {
		t.Fatal("empty anchor pattern judged without error")
	}
}

type stubChecker struct{}

func (stubChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
