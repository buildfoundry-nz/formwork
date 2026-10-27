package fixturetest_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/fixturetest"
)

// A pass fixture standing at an except.paths entry its author declared inert
// with `# except-declaration: <reason>` is excused — the same excuse the census
// reads — and one without a reason (or without the marker) is still vacuous.
func exceptRules(marker string) string {
	if marker != "" {
		marker = "        " + marker + "\n"
	}
	return "rules:\n" +
		"  - id: decl-rule\n" +
		"    type: forbidden-pattern\n" +
		"    scope:\n" +
		"      include: [\"src/**/*.go\"]\n" +
		"    except:\n" +
		"      paths:\n" +
		marker +
		"        - \"src/decl.go\"\n" +
		"    params:\n" +
		"      pattern: 'BANNED'\n"
}

func declResult(t *testing.T, marker string) fixturetest.RuleResult {
	t.Helper()
	return ruleByID(t, vacuityResults(t, map[string]string{
		".formwork/rules/r.yaml":                          exceptRules(marker),
		".formwork/fixtures/decl-rule/fire-1/src/a.go":    "package a\n\nvar x = BANNED // want: decl-rule\n",
		".formwork/fixtures/decl-rule/pass-1/src/decl.go": "package a\n\nvar y = fine\n",
	}), "decl-rule")
}

func TestDeclaredInertExceptPathExcusesAPassFixture(t *testing.T) {
	rr := declResult(t, "# except-declaration: generic over every kind; the pattern cannot appear")
	if rr.Status != fixturetest.StatusOK {
		t.Fatalf("status = %q, want ok: %+v", rr.Status, rr)
	}
}

func TestBareExceptDeclarationDoesNotExcuseAPassFixture(t *testing.T) {
	rr := declResult(t, "# except-declaration:")
	if rr.Status != fixturetest.StatusFail || !strings.Contains(strings.Join(armByName(t, rr, "pass-1").Problems, "\n"), "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("want fail with PASS-FIXTURE-VACUOUS: %+v", rr)
	}
}

func TestUndeclaredExceptPathStillFailsAsVacuous(t *testing.T) {
	rr := declResult(t, "")
	if rr.Status != fixturetest.StatusFail || !strings.Contains(strings.Join(armByName(t, rr, "pass-1").Problems, "\n"), "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("want fail with PASS-FIXTURE-VACUOUS: %+v", rr)
	}
}
