// census_exceptpaths_test.go — #138.
//
// The escape-hatch census is the one report that answers "where are our
// exemptions". For except.paths it printed the entries a rule DECLARES and
// nothing else, so an entry whose subject moved away printed in the same shape
// and with the same weight as one doing real work. The report overstated the
// exemption surface and a reader could not tell live carve-outs from fossils.
//
// Both neighbouring channels in the same function already report effect:
// scan.ignore prints live match counts precisely so a typo'd glob shows
// "0 matches" forever, and suppressed findings are enumerated per rule. This
// was the channel that got neither — and the one where the gap cannot be closed
// by reading findings, because except.paths is a scope SUBTRACTION: Applies
// returns false, the rule never evaluates the file, and no finding exists to
// count.
package meta_test

import (
	"strings"
	"testing"
)

const exceptPathsRule = "rules:\n" +
	"  - id: no-ghost\n" +
	"    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    except:\n" +
	"      paths: ['legacy/**', 'gone/**']\n" +
	"    params: {pattern: 'Ghost'}\n" +
	"    cure: \"drop it\"\n"

func exceptPathsRepo(extra map[string]string) map[string]string {
	base := map[string]string{
		".formwork/formwork.yaml":                 "version: 1\n",
		".formwork/rules/r.yaml":                  exceptPathsRule,
		".formwork/fixtures/no-ghost/fire-1/a.go": "package p // Ghost want: no-ghost\n",
		".formwork/fixtures/no-ghost/pass-1/b.go": "package p\n",
		"src.go": "package p\n",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// A live entry reports what it removed; a dead one reports zero rather than
// printing identically to the live one.
func TestCensusReportsExceptPathsEffectNotDeclaration(t *testing.T) {
	_, out := lint(t, exceptPathsRepo(map[string]string{
		"legacy/old.go":   "package p // Ghost\n",
		"legacy/other.go": "package p\n",
	}))

	if !strings.Contains(out, "legacy/**") {
		t.Fatalf("the census must still name every declared entry:\n%s", out)
	}
	// `legacy/**` removed two in-scope .go files; `gone/**` removed none.
	if !strings.Contains(out, "legacy/**: 2 file(s)") {
		t.Fatalf("a live entry must report the files it actually removed:\n%s", out)
	}
	if !strings.Contains(out, "gone/**: 0 file(s)") {
		t.Fatalf("a dead entry must report 0, not print like a live one:\n%s", out)
	}
}

// The narrowing that makes the number mean something: a path the rule never
// covered was not carved out by anything. Without the include/exclude test
// first, a glob matching files outside the rule's scope would inflate the count
// and the census would overstate the surface in a new way.
func TestCensusExceptPathsCountsOnlyInScopeFiles(t *testing.T) {
	_, out := lint(t, exceptPathsRepo(map[string]string{
		"legacy/old.go":    "package p // Ghost\n",
		"legacy/notes.md":  "not a go file, never in scope\n",
		"legacy/data.json": "{}\n",
	}))
	if !strings.Contains(out, "legacy/**: 1 file(s)") {
		t.Fatalf("only files the rule's scope covered may be counted:\n%s", out)
	}
}
