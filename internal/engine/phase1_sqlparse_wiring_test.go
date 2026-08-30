// phase1_sqlparse_wiring_test.go — the phase-1 partition must classify the REAL
// registered checkers, not only the fakes in phase1_pool_test.go that prove the
// mechanism works.
//
// Why this file exists: a partition nothing ships into is a mechanism that runs
// on every invocation and changes nothing. #83's bound lives in
// internal/rules/sqlparse/parser.go — `parseSem <- struct{}{}`, a BLOCKING send
// on the calling goroutine — and EVERY registered sqlparse rule type reaches it
// synchronously from CheckFile:
//
//	sql/parses                parses.go       -> statements        -> parseChunk -> parse
//	sql/locking-select-order  locking.go      -> lockingStatements -> parseChunk -> parse
//	sql/locking-target        lockingtarget.go-> lockingStatements -> parseChunk -> parse
//
// so each of the three parks the phase-1 worker that called it, and each must
// be dispatched through the bounded half. Measured before this wiring landed,
// with the partition already in place: 200 .sql files under sql/parses plus
// 4,000 .txt files under forbidden-pattern at bound 4 / --workers 12 cost
// sql=0.62s, txt=0.36s, mixed=0.99s — still sum(), because the shipped rule sat
// in the fast half and the two pools were parser-bound and fast in the same one.
//
// COST IS NOT THE DISCRIMINATOR, and the negative half here is what pins that
// instead of asserting it in prose. sql/locking-target declares Cost() CostFast
// explicitly and the other two default to CostFast, so a partition keyed on
// CostHeavy would route all three into the WRONG half while sweeping `command`
// — CostHeavy, forks a subprocess, never touches a parser — into the bounded
// one. Both crossings are asserted below.
//
// Registry-driven on purpose. It asserts what the ENGINE does with the checkers
// a real config produces, end to end, rather than that some method exists on a
// type: a renamed or restructured sqlparse type that stopped reaching the
// bounded half would still satisfy a method-presence check.
package engine_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/rules"

	// The registrations under test. sqlparse and sqltext are the only packages
	// that register a `sql/*` type, which is what lets the coverage test below
	// enumerate the whole SQL family from the registry without depending on
	// which other rule packages another test file happens to import.
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
)

// sqlparsePkg is the import path whose CheckFile paths reach parseSem. The
// positive cases assert their checker really comes from here, so a stub
// registered under one of these names elsewhere cannot satisfy this test.
const sqlparsePkg = "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"

// parserBoundTypes are the registered rule types that enter sqlparse's WASM
// parser from CheckFile. Params are the minimum each factory accepts — these
// rules are never evaluated here, only built and classified.
var parserBoundTypes = []struct{ typ, params string }{
	{"sql/parses", "{}"},
	{"sql/locking-select-order", "unique_key_columns: [id]"},
	{"sql/locking-target", "table: projects\nstrength: [update]"},
}

// fullWidthTypes are registered rule types that must NOT be dispatched through
// the bounded half. Each `why` is the reason it reaches no parser, and the
// reasons are deliberately different: a pure-Go SQL rule, an ordinary text
// rule, and a heavy subprocess rule.
var fullWidthTypes = []struct{ typ, params, why string }{
	{
		"sql/statement-predicate", "table: projects\nrequire: [where]",
		"sqltext splits statements in pure Go and never enters sqlparse's parser",
	},
	{
		"forbidden-pattern", "pattern: zzz-no-such-token",
		"it is an ordinary per-file text scan that blocks on nothing",
	},
	{
		"command", `cmd: ["echo", "ok"]`,
		"its resource is a subprocess, governed by phase 2's heavy pools and the " +
			"machine-wide gate rather than by a parser semaphore — CostHeavy is not " +
			"what this partition reads",
	},
}

// TestPhase1RoutesEveryParserBoundRuleToTheBoundedHalf is the wiring half of
// #315: without it the partition is built and never used by a shipped rule,
// which is a mechanism that cannot change any run.
//
// It also carries the CostFast side of the cost-independence property: at least
// one type in the bounded half must be CostFast, or "Cost is not the
// discriminator" would be a claim this file no longer tests.
func TestPhase1RoutesEveryParserBoundRuleToTheBoundedHalf(t *testing.T) {
	fastAndBound := 0
	for _, tc := range parserBoundTypes {
		t.Run(tc.typ, func(t *testing.T) {
			c := checkerWithParams(t, tc.typ, tc.params)
			if got := pkgPathOf(c); got != sqlparsePkg {
				t.Fatalf("%s built a %T from %s, want a checker from %s: this test only says "+
					"anything about the parser bound if the checker it classifies is the one that "+
					"takes it", tc.typ, c, got, sqlparsePkg)
			}
			if !engine.SelfBoundedOf(c) {
				t.Fatalf("%s is not classified self-bounded, so phase 1 dispatches it through the "+
					"full-width half — but its CheckFile reaches sqlparse's `parseSem <- struct{}{}`, "+
					"a blocking send that PARKS the calling worker while it still holds its file. "+
					"Every other rule in that pool is then throttled to the parser's bound (#83 "+
					"acceptance criterion 2, #315)", tc.typ)
			}
			if rules.CostOf(c) == rules.CostFast {
				fastAndBound++
			}
		})
	}
	if fastAndBound == 0 {
		t.Fatalf("no parser-bound rule type is CostFast: the point of a separate discriminator is "+
			"that self-bounding is independent of cost, and with every bounded type gone heavy this "+
			"file would pass just as well against a Cost()-keyed partition (%d types checked)",
			len(parserBoundTypes))
	}
}

// TestPhase1LeavesNonParsingRulesAtFullWidth is the other half of the
// acceptance criterion — "non-SQL rules keep full parallelism" — at the
// classification level: a discriminator that said true for everything would
// satisfy the test above and reproduce the exact serialisation #315 reports,
// because one pool would again hold every rule.
//
// It carries the CostHeavy side of cost-independence: `command` must be
// CostHeavy AND not self-bounded, which is what makes Cost() the wrong
// discriminator a pinned property rather than a comment on the partition.
func TestPhase1LeavesNonParsingRulesAtFullWidth(t *testing.T) {
	sawHeavy := false
	for _, tc := range fullWidthTypes {
		t.Run(tc.typ, func(t *testing.T) {
			c := checkerWithParams(t, tc.typ, tc.params)
			if engine.SelfBoundedOf(c) {
				t.Fatalf("%s is classified self-bounded, but %s. Routing it into the bounded half "+
					"puts it behind a semaphore it never needed; routing EVERYTHING there restores "+
					"the single throttled pool #315 exists to split (#315)", tc.typ, tc.why)
			}
			if rules.CostOf(c) == rules.CostHeavy {
				sawHeavy = true
			}
		})
	}
	if !sawHeavy {
		t.Fatal("no full-width rule type is CostHeavy: without one, nothing here rules out a " +
			"partition keyed on Cost() CostHeavy, which would sweep every heavy subprocess rule " +
			"into a pool sized for a WASM parser")
	}
}

// TestPhase1ClassifiesEverySQLRuleType is the completeness guard. parseSem is
// reachable only from package sqlparse, and every rule type sqlparse registers
// is named `sql/*` — so enumerating the registry's `sql/*` family catches a new
// SQL rule type that shipped without anyone deciding which half of phase 1 it
// belongs in. Landing in the wrong half is silent: too-bounded is a slow run,
// too-wide is #315 again.
//
// The enumeration is complete regardless of what other test files import,
// because this file imports both packages that register a `sql/*` name.
func TestPhase1ClassifiesEverySQLRuleType(t *testing.T) {
	var registered []string
	for _, n := range rules.TypeNames() {
		if strings.HasPrefix(n, "sql/") {
			registered = append(registered, n)
		}
	}
	classified := make([]string, 0, len(parserBoundTypes)+len(fullWidthTypes))
	for _, tc := range parserBoundTypes {
		classified = append(classified, tc.typ)
	}
	for _, tc := range fullWidthTypes {
		if strings.HasPrefix(tc.typ, "sql/") {
			classified = append(classified, tc.typ)
		}
	}
	sort.Strings(registered)
	sort.Strings(classified)
	if strings.Join(registered, ",") != strings.Join(classified, ",") {
		t.Fatalf("registered sql/* rule types %v, classified by this file %v: every SQL rule type "+
			"must be pinned to one half of the phase-1 partition. Add it to parserBoundTypes if its "+
			"CheckFile reaches sqlparse's parser, to fullWidthTypes if it does not (#315)",
			registered, classified)
	}
}

// pkgPathOf returns the import path that declares c's concrete type, following
// one pointer indirection.
func pkgPathOf(c rules.Checker) string {
	t := reflect.TypeOf(c)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.PkgPath()
}
