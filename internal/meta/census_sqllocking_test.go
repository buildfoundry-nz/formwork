// census_sqllocking_test.go — the lint-layer pin #311 was missing.
//
// #311 was closed once already and reopened: sqlparse.CensusSites was built,
// tested, and never called, so `formwork lint` kept asking sqlextract.FromGo
// behind a `sql/` type-name prefix. Every test holding the seam lived in
// internal/rules/sqlparse and passed against a function the lint path did not
// reach — which is exactly how the gap survived a merged PR.
//
// So the pin has to be HERE, in the package that renders the census, and it has
// to fail when this package stops routing through the seam. The mutation that
// proves it: narrow census.go back to `sqlextract.FromGo` behind a prefix match,
// and this file must go red.
package meta

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
)

// TestTheCensusRoutesLockingRulesThroughTheSeam holds the property #311 is
// about, at the altitude it is about.
//
// The locking rule types source their SQL through FromGoReassembled, which
// resolves two composition shapes FromGo declines. Asking FromGo about them
// made the census wrong in both directions at once: it printed "not analysed by
// this rule" about lines the rule reads and fires on, and printed nothing about
// compositions the rule genuinely cannot read.
//
// Asserted through the seam's own registry rather than a hardcoded list, so a
// newly registered sql/* type is covered on the day it registers.
func TestTheCensusRoutesLockingRulesThroughTheSeam(t *testing.T) {
	for _, ruleType := range []string{"sql/locking-select-order", "sql/locking-target"} {
		if !sqlparse.AccountedForByTheCensus(ruleType) {
			t.Errorf("%s is not in sqlparse's extractor mapping, so CensusSites refuses it and "+
				"the census this package renders has no answer for a rule type the engine "+
				"registers. A sql/* type the census cannot source is the #311 defect with a "+
				"different type name.", ruleType)
		}
	}
}

// TestCensusSourcesTheExtractorTheRuleUses is the one that reddens on the
// mutation. It drives the real seam over a composition FromGo declines and
// FromGoReassembled reads — a fmt.Sprintf with a literal first argument — and
// asserts the census says NOTHING about it for a locking rule.
//
// Under the pre-#311 wiring the same input produced a census line calling that
// line "not analysed by this rule", while `formwork check` was failing on it.
// A census that contradicts the check on the same line is worse than no census:
// it tells an operator holding a real finding that the finding was not analysed.
func TestCensusSourcesTheExtractorTheRuleUses(t *testing.T) {
	const src = `package db

import "fmt"

func q(tbl string) string {
	return fmt.Sprintf("SELECT id FROM %s FOR UPDATE", tbl)
}
`
	sites, ok, err := sqlparse.CensusSites("sql/locking-select-order", "db/q.go", []byte(src))
	if !ok {
		t.Fatal("CensusSites answered \"not a SQL rule\" for sql/locking-select-order — the census " +
			"would then owe this rule no line at all, which is the silent half of #311")
	}
	if err != nil {
		t.Fatalf("CensusSites: %v", err)
	}
	for _, s := range sites {
		if strings.HasSuffix(s.Path, "db/q.go") {
			t.Errorf("the census reports %s:%d as unreadable (%s), but the locking rules source "+
				"through FromGoReassembled, which RESOLVES a fmt.Sprintf with a literal first "+
				"argument and analyses it. `formwork check` fires on this line. A census line "+
				"here calls the line the check just failed on \"not analysed by this rule\" — "+
				"#311, in the direction that misdirects triage.", s.Path, s.Line, s.Reason)
		}
	}
}

// TestCensusRefusesARegisteredTypeItCannotSource pins the fail-closed half. A
// sql/* type in neither extractor table must be refused, not handed FromGo's
// answer — a guess rendered as a census line is indistinguishable from a fact.
func TestCensusRefusesARegisteredTypeItCannotSource(t *testing.T) {
	_, ok, err := sqlparse.CensusSites("sql/not-a-real-type", "db/q.go", []byte("package db\n"))
	if !ok {
		t.Fatal("CensusSites answered \"not a SQL rule\" for a sql/-prefixed type — the caller " +
			"asked the right question and is owed a refusal, not a shrug")
	}
	if err == nil {
		t.Error("CensusSites accepted a sql/* type in neither extractor table and returned no " +
			"error, so the census would render whichever extractor happened to be asked. " +
			"An unknown type is a gap the census owes a line, not a silent fallback.")
	}
}
