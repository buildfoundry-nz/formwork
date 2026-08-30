// fixture_exempt_test.go — #53.
//
// A heavy (command / git-diff) rule was fixture-exempt by COST: nothing
// required it to carry fixtures, and `formwork test` printed
// `SKIP — no fixtures` and moved on. Command rules are the escape hatch used
// for the highest-stakes lockdowns, so the rules with no firing proof at all
// were the ones that most needed it — and nothing distinguished "cannot be
// fixtured by construction" from "nobody bothered". Downstream, 58 rules were
// in that state and the run exited 0.
//
// The exemption is now DECLARED, not inferred, which is this repo's pattern
// everywhere else: scan.ignore takes a mandatory reason per glob, a lint.yaml
// skip takes a reason per entry, a dead scope.exclude needs a justification
// comment. An undeclared one is reported rather than skipped in silence.
package meta_test

import (
	"github.com/buildfoundry-nz/formwork/internal/config"
	"strings"
	"testing"
)

func heavyRule(extra string) string {
	return "rules:\n" +
		"  - id: shell-gate\n" +
		"    type: command\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    params: {cmd: ['true']}\n" +
		extra +
		"    cure: \"run the gate\"\n"
}

func heavyRepo(extra string) map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  heavyRule(extra),
		"src.go":                  "package p\n",
	}
}

// The defect: no fixtures, no declaration, and the check said nothing.
func TestFixtureCoverageReportsAnUndeclaredHeavyExemption(t *testing.T) {
	failed, out := lint(t, heavyRepo(""))
	if failed == 0 {
		t.Fatalf("a heavy rule with neither fixtures nor a declared exemption must be reported:\n%s", out)
	}
	if !strings.Contains(out, "shell-gate") {
		t.Fatalf("the report must name the rule, got:\n%s", out)
	}
}

// Declaring it clears the report — the declaration is the cure, so it has to
// work, or the check is a wall rather than a gate.
func TestFixtureCoverageAcceptsADeclaredHeavyExemption(t *testing.T) {
	failed, out := lint(t, heavyRepo("    fixture_exempt: \"drives git state a fixture tree cannot reproduce\"\n"))
	if failed != 0 {
		t.Fatalf("a declared exemption must satisfy the check:\n%s", out)
	}
}

// An escape hatch that is declared must also be VISIBLE — that is the whole
// doctrine of the census. A declaration that silenced the check without
// appearing anywhere would just be a quieter version of the original defect.
func TestCensusEnumeratesADeclaredHeavyExemption(t *testing.T) {
	_, out := lint(t, heavyRepo("    fixture_exempt: \"drives git state a fixture tree cannot reproduce\"\n"))
	if !strings.Contains(out, "fixture-exempt") || !strings.Contains(out, "drives git state") {
		t.Fatalf("the census must enumerate the declared exemption and its reason:\n%s", out)
	}
}

// fastRepo is heavyRepo's opposite number: a NON-heavy (forbidden-pattern)
// rule, which fixture-coverage judges on fire/pass fixtures and which
// `fixture_exempt` does not govern at all — docs/reference.md:186 says the
// field is "Heavy rules only".
func fastRepo(extra string) map[string]string {
	return map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-widget\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {pattern: WIDGET}\n" +
			extra +
			"    cure: \"remove it\"\n",
		"notes.txt": "in scope\n",
	}
}

// #336. The gate exists so "the gap is a decision instead of an accident"
// (fixturecoverage.go:48), and three spaces are neither. A whitespace-only
// declaration bought the exemption outright: fixture-coverage reported the
// heavy rule clean and `formwork lint` exited 0. The house predicate was
// already owned twice next door — internal/config/scan.go:94 and :115 refuse a
// scan reason on `strings.TrimSpace(...) == ""`, and internal/meta's own
// lintpolicy.go:100-106 refuses a lint.yaml skip the same way — and this one
// field skipped it.
// SUPERSEDED IN PLACE by #336's parse-time refusal. This was written when a
// content-free declaration LOADED and had to be caught downstream by
// fixture-coverage; internal/config now refuses it where it is read, which is
// what #336's last comment asked for and is strictly stronger — no consumer
// can inherit the hazard. The property is unchanged (three spaces buy
// nothing), so the assertion moves to the refusal rather than being deleted.
func TestFixtureCoverageRejectsAContentFreeHeavyExemption(t *testing.T) {
	root := writeRepo(t, heavyRepo("    fixture_exempt: \"   \"\n"))
	_, err := config.Load(root)
	if err == nil {
		t.Fatal("a whitespace-only fixture_exempt loaded — it declares nothing and must not " +
			"buy the exemption at any layer")
	}
	if !strings.Contains(err.Error(), "declares nothing") {
		t.Fatalf("the refusal must say what is wrong, got: %v", err)
	}
	if !strings.Contains(err.Error(), "shell-gate") {
		t.Fatalf("the refusal must name the rule so an operator can find it, got: %v", err)
	}
}

// The census's whole job is naming WHICH decision was made (#230). A line
// reading `shell-gate: fixture-exempt (declared):` with nothing after the
// colon names none, so it is not a disclosure — it is the same accident in a
// quieter spelling (#336).
// Same supersession: the census cannot enumerate what the loader refused to
// load, so the absence this asserted is now guaranteed one layer earlier.
func TestCensusDoesNotEnumerateAContentFreeExemption(t *testing.T) {
	root := writeRepo(t, heavyRepo("    fixture_exempt: \"   \"\n"))
	if _, err := config.Load(root); err == nil {
		t.Fatal("the config loaded, so the census would be asked to enumerate a declaration " +
			"with no content — the same accident in a quieter spelling")
	}
}

// The secondary half of #336, which needs no adversary: `fixture_exempt` on a
// NON-heavy rule is inert — fixture-coverage still demands its fixtures — yet
// the census enumerated it as an exemption in force two lines below the
// failure for that same rule. An escape hatch that is not in force must not be
// listed among the ones that are.
func TestCensusDoesNotEnumerateAnInertExemptionOnAFastRule(t *testing.T) {
	failed, out := lint(t, fastRepo("    fixture_exempt: \"I just don't want fixtures\"\n"))
	if failed == 0 || !strings.Contains(out, "no-widget: no fire fixture") {
		t.Fatalf("a fast rule's exemption is inert: fixture-coverage must still demand its fixtures:\n%s", out)
	}
	if strings.Contains(out, "fixture-exempt (declared)") {
		t.Fatalf("an inert declaration must not be enumerated as an exemption in force:\n%s", out)
	}
}
