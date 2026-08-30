package pattern_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

func TestPrefilterImplied(t *testing.T) {
	cases := []struct {
		name      string
		params    string
		implied   bool
		decidable bool
		wantCE    string // substring of the counterexample, when one is expected
	}{{
		name:      "every alternative carries the literal",
		params:    "pattern: '\\bAlphaOne\\b|\\bAlphaTwo\\b'\nprefilter: Alpha\n",
		implied:   true,
		decidable: true,
	}, {
		name:      "one alternative cannot contain it",
		params:    "pattern: '\\bAlphaOne\\b|\\bbeta-two\\b'\nprefilter: Alpha",
		implied:   false,
		decidable: true,
		wantCE:    "beta-two",
	}, {
		// The escaping case the 2026-07-27 design named when it rejected a
		// lexical test: the SOURCE has a backslash the literal does not, so a
		// substring comparison against the source text says "absent" and
		// reports a rule that is in fact perfectly gated.
		name:      "escaped metacharacter resolves to the literal it denotes",
		params:    "pattern: 'platform\\.material_cat'\nprefilter: platform.material_cat\n",
		implied:   true,
		decidable: true,
	}, {
		name:      "plain literal containing the prefilter",
		params:    "pattern: 'GhostRouteHandler'\nprefilter: GhostRoute\n",
		implied:   true,
		decidable: true,
	}, {
		// The gate is a case-SENSITIVE strings.Contains, so nothing may be
		// concluded from a case-folded literal in either direction.
		name:      "case-folded pattern is undecidable",
		params:    "pattern: '(?i)alphaone'\nprefilter: Alpha\n",
		decidable: false,
	}, {
		name:      "regexp2 syntax is declined outright",
		params:    "pattern: 'Alpha(?!Two)'\nsyntax: regexp2\nprefilter: Alpha\n",
		decidable: false,
	}, {
		// A quantified group may match zero times, so it guarantees nothing;
		// the only guaranteed run is "x", which lacks the literal.
		name:      "optional group cannot guarantee the literal",
		params:    "pattern: 'x(Alpha)?'\nprefilter: Alpha\n",
		implied:   false,
		decidable: true,
	}, {
		name:      "no prefilter at all is not a verdict",
		params:    "pattern: '\\bAlphaOne\\b'\n",
		decidable: false,
	}, {
		// Measured false positive on a ported corpus. The trigger alone cannot
		// imply the literal, but the require_present guard must ALSO match for
		// the rule to fire, and it necessarily contains it — so the prefilter
		// is pure. This is the most idiomatic prefilter shape there is: the
		// guard is where the file-level fact lives.
		name: "require_present guard implies the prefilter",
		params: "pattern: '\\.broadcast\\('\n" +
			"require_present: ['(?:implements|extends|with)\\s+[A-Za-z]*ActivitySource']\n" +
			"prefilter: ActivitySource\n",
		implied:   true,
		decidable: true,
	}, {
		// ...but a guard that does NOT carry the literal must not rescue it.
		name: "require_present guard without the literal still reports",
		params: "pattern: 'beta-two'\n" +
			"require_present: ['gamma']\n" +
			"prefilter: Alpha\n",
		implied:   false,
		decidable: true,
		wantCE:    "beta-two",
	}, {
		// require_absent asserts what must NOT be there, so it can never
		// guarantee the literal appears — it is not a conjunct.
		name: "require_absent cannot imply the prefilter",
		params: "pattern: 'beta-two'\n" +
			"require_absent: ['Alpha']\n" +
			"prefilter: Alpha\n",
		implied:   false,
		decidable: true,
		wantCE:    "beta-two",
	}, {
		// all_of is a conjunction: every pattern must match, so one of them
		// requiring the literal requires it of the rule.
		name:      "all_of implied by a single conjunct",
		params:    "all_of: ['\\bAlphaOne\\b', 'unrelated']\nprefilter: Alpha\n",
		implied:   true,
		decidable: true,
	}, {
		name:      "all_of with no conjunct requiring it",
		params:    "all_of: ['beta-two', 'gamma']\nprefilter: Alpha\n",
		implied:   false,
		decidable: true,
		wantCE:    "beta-two",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustChecker(t, "forbidden-pattern", tc.params)
			implied, decidable, ce := rules.PrefilterImpliedBy(c)
			if decidable != tc.decidable {
				t.Fatalf("decidable=%v want %v (implied=%v ce=%q)", decidable, tc.decidable, implied, ce)
			}
			if !tc.decidable {
				return
			}
			if implied != tc.implied {
				t.Fatalf("implied=%v want %v (ce=%q)", implied, tc.implied, ce)
			}
			if tc.wantCE != "" && !contains(ce, tc.wantCE) {
				t.Fatalf("counterexample %q does not name %q", ce, tc.wantCE)
			}
			if tc.implied && ce != "" {
				t.Fatalf("an implied prefilter must carry no counterexample, got %q", ce)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
