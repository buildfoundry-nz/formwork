package meta_test

import (
	"strings"
	"testing"
)

// A tombstone rule bans the distinctive names of a removed feature, so by
// construction NOTHING on the tree matches it. That makes the real-tree
// differential blind: stripping the prefilter changes nothing, because both
// sides are empty. The rule's own fire fixtures are the evidence the tree
// cannot supply — they exist precisely to contain what the rule bans (#133).
const tombstoneRule = "rules:\n" +
	"  - id: no-ghost\n" +
	"    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    params: {pattern: '\\bAlphaOne\\b|\\bbeta-two\\b', prefilter: Alpha}\n"

// fire-2 holds the branch the prefilter drops: 'beta-two' does not contain
// the literal "Alpha", so with the prefilter live this fixture cannot fire.
var tombstoneFixtures = map[string]string{
	".formwork/fixtures/no-ghost/fire-1/a.go": "package p // AlphaOne want: no-ghost\n",
	".formwork/fixtures/no-ghost/fire-2/b.go": "package p // beta-two want: no-ghost\n",
	".formwork/fixtures/no-ghost/pass-1/c.go": "package p\n",
}

func withTombstoneFixtures(files map[string]string) map[string]string {
	for k, v := range tombstoneFixtures {
		files[k] = v
	}
	return files
}

func TestLintFlagsLoadBearingPrefilterOnZeroFindingRule(t *testing.T) {
	// The tree contains NEITHER banned name, so the real-tree differential has
	// nothing to compare and passes vacuously. The fixture differential has
	// evidence: stripping the prefilter makes fire-2 match.
	failed, out := lint(t, withTombstoneFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  tombstoneRule,
		"main.go":                 "package p\nfunc main() {}\n",
	}))
	if failed == 0 {
		t.Fatalf("a prefilter that drops a fixture-covered branch must fail the check; got failed=0\n%s", out)
	}
	for _, want := range []string{
		"[prefilter-load-bearing] FAIL",
		`no-ghost: prefilter "Alpha" is load-bearing`,
		"fire-2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintPassesPrefilterImpliedByEveryBranch(t *testing.T) {
	// Every alternative contains the literal, so no match is possible without
	// it: the prefilter is provably a pure optimization. No fixture can
	// disagree, and the static arm must not report it as unproven either.
	failed, out := lint(t, withTombstoneFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.go']}\n" +
			"    params: {pattern: '\\bAlphaOne\\b|\\bAlphaTwo\\b', prefilter: Alpha}\n",
		"main.go": "package p\nfunc main() {}\n",
	}))
	if !strings.Contains(out, "[prefilter-load-bearing] OK") {
		t.Fatalf("failed=%d, want the check OK:\n%s", failed, out)
	}
}

func TestLintReportsUnprovenPrefilterWithNoEvidence(t *testing.T) {
	// No fixtures, and a regexp2 pattern the static arm cannot parse: there is
	// no evidence in either direction. Passing here would be the "a check that
	// skips is a check that passes" failure this corpus is held to.
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-ghost\n" +
			"    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.go']}\n" +
			"    params: {pattern: 'Alpha(?!Two)', syntax: regexp2, prefilter: Alpha}\n",
		"main.go": "package p\nfunc main() {}\n",
	})
	for _, want := range []string{
		"[prefilter-load-bearing] FAIL",
		`no-ghost: prefilter "Alpha" is unproven`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
