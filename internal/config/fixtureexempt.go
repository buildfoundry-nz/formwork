package config

import (
	"fmt"
	"strings"
)

// fixtureExemptReason validates a rule's `fixture_exempt` declaration at load
// time and returns the reason to store.
//
// The field is a heavy rule's DECLARED reason for carrying no fixtures (#53),
// and it used to reach the engine as a raw assignment — no trim, no
// validation — so any non-empty byte sequence bought the exemption.
// `fixture_exempt: "   "` turned fixture-coverage from FAIL/exit 1 into
// OK/exit 0 and printed `<id>: fixture-exempt (declared):` on the escape-hatch
// census: a line with nothing after the colon, on the surface whose whole job
// is naming WHICH decision was made (#230). The gate's claim is that the gap
// is a decision (internal/meta/fixturecoverage.go); three spaces decide
// nothing. Refused at load, exit 2, where this package already refuses the two
// sibling reasons — scan.ignore (scan.go:94) and scan.gitignore (scan.go:115).
//
// TrimSpace, not the alnum predicate internal/marker applies to `formwork:allow`
// reasons. That predicate earns its keep there because a marker reason is
// scraped out of a comment, so trimming alone leaves a trailing `*/` reading as
// content; a YAML scalar carries no such wrapper. TrimSpace is also what the
// field's two readers apply to it — internal/meta/fixturecoverage.go and
// census.go — so the loader now refuses exactly what they would have ignored.
//
// Present-and-empty is deliberately NOT refused. `fixture_exempt: ""` and an
// absent key decode to the same Go value, so nothing here can tell them apart,
// and both are already the undeclared state that fixture-coverage reports
// rather than skips.
//
// Neither is a `fixture_exempt` on a FAST rule, and that is a decision rather
// than an omission. The field governs heavy rules only, and the rule-anatomy
// example in docs/reference.md writes it on a `forbidden-pattern` rule tagged
// `[fast, go]`, so refusing it here would refuse the shape the manual teaches.
// What such a declaration must not do is READ as an exemption in force on a
// rule it does not govern, and #336 answers that at both read sites instead:
// `formwork test` skips such a rule as undeclared (internal/fixturetest/run.go)
// and the escape-hatch census enumerates a declaration only when it is in force
// (internal/meta/census.go). fixture-coverage judges a fast rule on its fire and
// pass fixtures whatever it declares, so the verdict does not move either way.
func fixtureExemptReason(ruleID, declared string) (string, error) {
	reason := strings.TrimSpace(declared)
	if declared != "" && reason == "" {
		return "", fmt.Errorf("rule %s: fixture_exempt is %q, which declares nothing — the field "+
			"records WHY a heavy rule carries no fixtures and the escape-hatch census enumerates "+
			"it with that reason; give the reason, or drop the field and let fixture-coverage "+
			"report the gap", ruleID, declared)
	}
	return reason, nil
}
