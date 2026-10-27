package fixturetest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
)

// Pass-fixture vacuity through Results (#49 S3): AC-5, each verdict has a fire
// test (the pass arm that must fail) and a pass test (the control that must not).

const vacuityRules = `rules:
  - id: ban-scoped
    type: forbidden-pattern
    scope:
      include: ["src/**/*.go"]
      exclude: ["src/generated/**"]
    params:
      pattern: 'BANNED'
  - id: confined
    type: go/call-confined-to-func-name
    scope:
      include: ["src/**/*.go"]
    params:
      symbol: '^marshalJob$'
      allowed_func: '^encodePayload$'
`

func vacuityResults(t *testing.T, files map[string]string) []fixturetest.RuleResult {
	t.Helper()
	files[".formwork/formwork.yaml"] = "version: 1\n"
	if _, ok := files[".formwork/rules/r.yaml"]; !ok {
		files[".formwork/rules/r.yaml"] = vacuityRules
	}
	root := writeRepo(t, files)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fixturetest.Results(cfg, fullIDs(cfg), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func ruleByID(t *testing.T, rs []fixturetest.RuleResult, id string) fixturetest.RuleResult {
	t.Helper()
	for _, r := range rs {
		if r.Rule == id {
			return r
		}
	}
	t.Fatalf("no result for rule %s in %+v", id, rs)
	return fixturetest.RuleResult{}
}

func armByName(t *testing.T, r fixturetest.RuleResult, name string) fixturetest.ArmResult {
	t.Helper()
	for _, a := range r.Arms {
		if a.Arm == name {
			return a
		}
	}
	t.Fatalf("no arm %s in %+v", name, r)
	return fixturetest.ArmResult{}
}

const (
	banFire = "package a\n\nvar x = BANNED // want: ban-scoped\n"
	cFire   = "package a\n\nfunc rogue() {\n\tmarshalJob() // want: confined\n}\n"
)

func TestVacuousPassArmFailsTheRule(t *testing.T) {
	rs := vacuityResults(t, map[string]string{
		".formwork/fixtures/ban-scoped/fire-1/src/a.go":           banFire,
		".formwork/fixtures/ban-scoped/pass-1/src/generated/g.go": "package g\n\nvar y = fine\n",
	})
	r := ruleByID(t, rs, "ban-scoped")
	if r.Status != fixturetest.StatusFail {
		t.Fatalf("status = %q, want fail: %+v", r.Status, r)
	}
	a := armByName(t, r, "pass-1")
	if a.OK || a.Vacuity != "vacuous" || a.VacuityReason == "" {
		t.Fatalf("pass-1 = %+v, want not-ok vacuous with a reason", a)
	}
	if len(a.Problems) != 1 || !strings.HasPrefix(a.Problems[0], "pass-1: PASS-FIXTURE-VACUOUS: ") {
		t.Fatalf("problems = %q, want one 'pass-1: PASS-FIXTURE-VACUOUS: <reason>'", a.Problems)
	}
	if f := armByName(t, r, "fire-1"); f.Vacuity != "" || f.VacuityReason != "" {
		t.Fatalf("fire arm carries vacuity: %+v", f)
	}
}

func TestBlindPassArmFailsTheRule(t *testing.T) {
	rs := vacuityResults(t, map[string]string{
		".formwork/fixtures/confined/fire-1/src/a.go": cFire,
		".formwork/fixtures/confined/pass-1/src/a.go": "package a\n\nfunc encodePayload() {\n\tjson.Marshal(m)\n}\n",
	})
	r := ruleByID(t, rs, "confined")
	if r.Status != fixturetest.StatusFail {
		t.Fatalf("status = %q, want fail: %+v", r.Status, r)
	}
	a := armByName(t, r, "pass-1")
	if a.OK || a.Vacuity != "blind" {
		t.Fatalf("pass-1 = %+v, want not-ok blind", a)
	}
	if len(a.Problems) != 1 || !strings.HasPrefix(a.Problems[0], "pass-1: PASS-FIXTURE-BLIND: ") {
		t.Fatalf("problems = %q", a.Problems)
	}
}

func TestNonVacuousPassArmsAreJudgedAndOK(t *testing.T) {
	rs := vacuityResults(t, map[string]string{
		".formwork/fixtures/ban-scoped/fire-1/src/a.go": banFire,
		".formwork/fixtures/ban-scoped/pass-1/src/a.go": "package a\n\nvar y = fine\n",
		".formwork/fixtures/confined/fire-1/src/a.go":   cFire,
		".formwork/fixtures/confined/pass-1/src/a.go":   "package a\n\nfunc encodePayload() {\n\tmarshalJob()\n}\n",
	})
	for _, id := range []string{"ban-scoped", "confined"} {
		r := ruleByID(t, rs, id)
		if r.Status != fixturetest.StatusOK {
			t.Fatalf("%s status = %q: %+v", id, r.Status, r)
		}
		if a := armByName(t, r, "pass-1"); !a.OK || a.Vacuity != "judged" || a.VacuityReason != "" {
			t.Fatalf("%s pass-1 = %+v, want ok judged", id, a)
		}
	}
}

func TestCommandRulePassArmIsNotJudgedAndOK(t *testing.T) {
	rs := vacuityResults(t, map[string]string{
		".formwork/rules/r.yaml": `rules:
  - id: runs-tool
    type: command
    scope:
      include: ["**"]
    params:
      cmd: ["true"]
`,
		".formwork/fixtures/runs-tool/pass-1/x.txt": "x\n",
	})
	r := ruleByID(t, rs, "runs-tool")
	if r.Status != fixturetest.StatusOK {
		t.Fatalf("status = %q: %+v", r.Status, r)
	}
	a := armByName(t, r, "pass-1")
	if !a.OK || a.Vacuity != "not-judged" || a.VacuityReason == "" {
		t.Fatalf("pass-1 = %+v, want ok not-judged with a reason", a)
	}
}

func TestCompileOnceCommandPassArmIsNotJudged(t *testing.T) {
	files := map[string]string{
		".formwork/rules/r.yaml": `rules:
  - id: det-once
    type: command
    scope:
      include: ["MARKER"]
    params:
      cmd: [go, run, scripts/dev/check-det.go]
`,
		".formwork/fixtures/det-once/fire-1/MARKER": "fire\n",
		".formwork/fixtures/det-once/fire-1.want":   "- detector-fired\n",
		".formwork/fixtures/det-once/pass-1/MARKER": "pass\n",
	}
	// The detector must exist in each arm before Results runs; vacuityResults
	// loads the repo itself, so build it by hand here.
	files[".formwork/formwork.yaml"] = "version: 1\n"
	root := writeRepo(t, files)
	for _, arm := range []string{"fire-1", "pass-1"} {
		writeDetectorFile(t, filepath.Join(root, ".formwork", "fixtures", "det-once", arm, "scripts", "dev", "check-det.go"))
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := fixturetest.Results(cfg, fullIDs(cfg), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	r := ruleByID(t, rs, "det-once")
	if r.Status != fixturetest.StatusOK {
		t.Fatalf("status = %q: %+v", r.Status, r)
	}
	if a := armByName(t, r, "pass-1"); !a.OK || a.Vacuity != "not-judged" || a.VacuityReason == "" {
		t.Fatalf("pass-1 = %+v, want ok not-judged", a)
	}
	if f := armByName(t, r, "fire-1"); f.Vacuity != "" {
		t.Fatalf("fire arm carries vacuity: %+v", f)
	}
}

// On the compile-once path a pass arm that already fails is not stamped
// not-judged either: vacuity is reported for arms that behaved.
func TestCompileOncePassArmThatFailsCarriesNoVacuity(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: det-once
    type: command
    scope:
      include: ["MARKER"]
    params:
      cmd: [go, run, scripts/dev/check-det.go]
`,
		".formwork/fixtures/det-once/fire-1/MARKER": "fire\n",
		".formwork/fixtures/det-once/fire-1.want":   "- detector-fired\n",
		// The detector fires on this "pass" arm, so it fails on its own.
		".formwork/fixtures/det-once/pass-1/MARKER": "fire\n",
	})
	for _, arm := range []string{"fire-1", "pass-1"} {
		writeDetectorFile(t, filepath.Join(root, ".formwork", "fixtures", "det-once", arm, "scripts", "dev", "check-det.go"))
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := fixturetest.Results(cfg, fullIDs(cfg), root, 2)
	if err != nil {
		t.Fatal(err)
	}
	a := armByName(t, ruleByID(t, rs, "det-once"), "pass-1")
	if a.OK || a.Vacuity != "" || a.VacuityReason != "" {
		t.Fatalf("pass-1 = %+v, want a failing arm with no vacuity", a)
	}
}

// A pass arm that already fails for its own reason is not also judged: the
// contract evaluates vacuity only for arms that are OK.
func TestFailingPassArmIsNotAlsoJudged(t *testing.T) {
	rs := vacuityResults(t, map[string]string{
		".formwork/fixtures/ban-scoped/fire-1/src/a.go": banFire,
		".formwork/fixtures/ban-scoped/pass-1/src/a.go": "package a\n\nvar y = BANNED\n",
	})
	a := armByName(t, ruleByID(t, rs, "ban-scoped"), "pass-1")
	if a.OK || a.Vacuity != "" || len(a.Problems) != 1 || strings.Contains(a.Problems[0], "PASS-FIXTURE") {
		t.Fatalf("pass-1 = %+v, want a plain unexpected-finding failure and no vacuity", a)
	}
}

func TestHumanOutputNamesNotJudgedPassFixtures(t *testing.T) {
	files := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": `rules:
  - id: runs-tool
    type: command
    scope:
      include: ["**"]
    params:
      cmd: ["true"]
`,
		".formwork/fixtures/runs-tool/pass-1/x.txt": "x\n",
	}
	root := writeRepo(t, files)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err != nil || failed != 0 {
		t.Fatalf("failed=%d err=%v\n%s", failed, err, sb.String())
	}
	want := "[runs-tool] OK — 1 fixture(s), 1 pass fixture(s) not vacuity-judged ("
	if !strings.Contains(sb.String(), want) {
		t.Fatalf("output lacks %q:\n%s", want, sb.String())
	}
}

func TestHumanOutputUnchangedWhenEveryPassArmIsJudged(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":                       "version: 1\n",
		".formwork/rules/r.yaml":                        vacuityRules,
		".formwork/fixtures/ban-scoped/fire-1/src/a.go": banFire,
		".formwork/fixtures/ban-scoped/pass-1/src/a.go": "package a\n\nvar y = fine\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if failed, err := fixturetest.Run(cfg, fullIDs(cfg), root, 2, &sb); err != nil || failed != 0 {
		t.Fatalf("failed=%d err=%v\n%s", failed, err, sb.String())
	}
	if !strings.Contains(sb.String(), "[ban-scoped] OK — 2 fixture(s)\n") {
		t.Fatalf("OK line changed for a fully judged rule:\n%s", sb.String())
	}
}
