// census_sites_test.go — #311.
//
// #75's census exists so an operator can ask "how many queries in this repo did
// the gate decline to analyse" and get a number rather than a doc comment. It
// asked sqlextract.FromGo, and neither locking rule sources through FromGo:
// both go through lockingStatements → sqlextract.FromGoReassembled, which
// RESOLVES fmt.Sprint{,f,ln} and one-sided '+' chains into fw_expr placeholder
// text and analyses them.
//
// So the channel was wrong in both directions at once, and this file pins both.
//
// THE FALSE CLAIM. FromGo emits a Site for exactly the two shapes
// FromGoReassembled resolves, so for a locking rule EVERY census line denied
// analysis of a composition the rule reads. One repo, one run: `formwork check`
// failing on db/q.go:6 with "locking SELECT over sibling rows has no
// deterministic ORDER BY" and `formwork lint` calling that same line "not
// analysed by this rule".
//
// THE BLINDNESS, which is the worse half. None of the fold's real limits
// produced a Site at all. Four files each hiding an unordered locking SELECT
// behind a strings.Builder, a loop, a called closure and `lockIt(&q)` gave exit
// 0 and a census listing nothing but the declared fixture_exempt — the channel
// reporting precisely what the rule DOES read and staying silent about
// everything it does not.
//
// The invariant is therefore two-sided, and it is measured against the SAME
// nineteen compositions locking_coverage_test.go runs through the real rule:
// a shape the block discloses SILENT must produce a site, and a shape it
// discloses FIRES or PASSES must produce none. Neither half is checkable
// alone — reporting everything satisfies the first, reporting nothing satisfies
// the second, and #75 shipped a channel that did the second while claiming the
// first.
//
// Subtested per shape on purpose. A narrowing of one arm of the fix has to
// redden the shape it narrows and nothing else, or the pin is a count rather
// than a claim.
package sqlparse_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// sitesFor runs the locking rules' own extractor over src and returns what it
// declined to read.
func sitesFor(t *testing.T, src string) []sqlextract.Site {
	t.Helper()
	sites, err := sqlparse.UnreadableSites("q.go", []byte(src))
	if err != nil {
		t.Fatalf("UnreadableSites: %v", err)
	}
	return sites
}

// A shape the block discloses SILENT is a query this rule did not read. The
// census is the only channel that says so, and a clean run on such a file means
// nothing without it.
func TestEverySilentShapeIsReportedAsUnreadable(t *testing.T) {
	partial := partialReasons()
	silent := 0
	for _, d := range disclosures(t, coverageBlock(t)) {
		if d.verdict != "SILENT" {
			continue
		}
		silent++
		src, ok := covShapes[d.key]
		if !ok {
			t.Fatalf("shape %q has no composition — locking_coverage_test.go "+
				"would have caught this first", d.key)
		}
		t.Run(d.key, func(t *testing.T) {
			got := sitesFor(t, src)
			if len(got) == 0 {
				t.Fatalf("the block discloses %q SILENT — no world folded, nothing "+
					"analysed — and the census channel reports nothing for it. That "+
					"is a coverage limit an operator cannot count, which is the "+
					"whole of #75's purpose and the whole of #311's defect.", d.key)
			}
			for _, s := range got {
				if s.Key == "" || s.Reason == "" {
					t.Fatalf("site %+v carries no shape key or no operator-facing "+
						"reason; the census renders the reason and a disclosure "+
						"without one is an unexplained silence", s)
				}
				if !strings.Contains(s.Text, "SELECT") {
					t.Fatalf("site %+v does not carry the query text it is about, so "+
						"a consumer cannot tell it from a declined import path", s)
				}
				if partial[s.Key] {
					t.Fatalf("site %+v claims this composition was read IN PART, and "+
						"the block discloses %q SILENT — no world was emitted at all, "+
						"so there is no part that was read", s, d.key)
				}
			}
		})
	}
	if silent < 8 {
		t.Fatalf("only %d SILENT shapes found in the block — this test's whole "+
			"corpus comes from there, so a block that stopped disclosing them "+
			"would make it pass vacuously", silent)
	}
}

// partialReasons are the reasons that say "read in part", keyed by shape.
func partialReasons() map[string]bool {
	out := map[string]bool{}
	for _, r := range sqlextract.UnreadableReasons() {
		if r.Partial {
			out[r.Key] = true
		}
	}
	return out
}

// The other direction, and the one #311 was filed about. A composition this
// rule reads is not a coverage limit, and reporting it as one tells an operator
// a rule declined to analyse the exact line the same run failed on.
//
// THERE ARE THREE CASES HERE, NOT TWO, and collapsing them is how #75 shipped.
// A shape can be one the fold read WHOLE (nothing to report), one it read not at
// all (SILENT, the test above), or one it read IN PART — a disqualified IIFE and
// a literal invoked in a statement header both drop their own appends while the
// variable outside stays tracked, so a world IS emitted and part of the query
// was never seen. The third case must be reported, or a clean run on a query
// whose only lock sits inside such a literal says nothing at all; and it must be
// reported with a reason that says "in part", or the census re-asserts the false
// claim in a new place.
//
// So a site on a shape the rule reads is admitted only when it carries a
// Partial reason AND names that shape's own construct. Everything else — a
// FromGo limit attributed here, a Partial reason stamped on a shape that has no
// such literal in it — fails.
func TestNoShapeTheRuleReadsIsReportedAsUnreadable(t *testing.T) {
	partial := partialReasons()
	read, partialShapes := 0, 0
	for _, d := range disclosures(t, coverageBlock(t)) {
		if d.verdict == "SILENT" {
			continue
		}
		read++
		if partial[d.key] {
			partialShapes++
		}
		src, ok := covShapes[d.key]
		if !ok {
			t.Fatalf("shape %q has no composition", d.key)
		}
		t.Run(d.key, func(t *testing.T) {
			for _, s := range sitesFor(t, src) {
				if !partial[s.Key] {
					t.Fatalf("the block discloses %q %s — the rule reads this "+
						"composition — and the census reports %q, a reason that "+
						"claims nothing here was analysed: %+v. That is `formwork "+
						"check` and `formwork lint` giving two contradictory answers "+
						"about one line in one run.", d.key, d.verdict, s.Key, s)
				}
				if s.Key != d.key {
					t.Fatalf("shape %q emitted a %q site: %+v — a partial-read reason "+
						"names a construct, and this composition does not contain "+
						"that one", d.key, s.Key, s)
				}
			}
		})
	}
	if read < 8 {
		t.Fatalf("only %d non-SILENT shapes in the block — the assertion above "+
			"would be nearly vacuous", read)
	}
	if partialShapes != len(partial) {
		t.Fatalf("%d partial-read reasons and %d shapes disclosed for them — every "+
			"one has to be a shape the block discloses as READ, or the vocabulary "+
			"admits sites no disclosure covers", len(partial), partialShapes)
	}
}

// The two vocabularies, tied. A reason's Partial flag is a claim about what the
// rule does with that construct, and the block is where that claim is measured:
// SILENT means no world was emitted, so a reason for a SILENT shape cannot be a
// partial read, and a reason for a shape that FIRES or PASSES must be.
func TestPartialFlagMatchesTheDisclosedVerdict(t *testing.T) {
	byKey := map[string]covDisclosure{}
	for _, d := range disclosures(t, coverageBlock(t)) {
		byKey[d.key] = d
	}
	checked := 0
	for _, r := range sqlextract.UnreadableReasons() {
		d, ok := byKey[r.Key]
		if !ok {
			t.Errorf("sqlextract can report %q and the COVERAGE LIMIT block "+
				"discloses no such shape", r.Key)
			continue
		}
		checked++
		if wantPartial := d.verdict != "SILENT"; wantPartial != r.Partial {
			t.Errorf("reason %q is Partial=%v and the block discloses that shape "+
				"%s — a reason that says the rule emitted nothing, for a shape the "+
				"rule fires on, is a census line contradicting the same run",
				r.Key, r.Partial, d.verdict)
		}
	}
	if checked != len(sqlextract.UnreadableReasons()) {
		t.Fatalf("only %d of %d reasons were checked", checked,
			len(sqlextract.UnreadableReasons()))
	}
}

// The dispatch itself, at the seam the census calls. Same file, same content,
// two rule types: the one that sources through FromGo gets FromGo's answer, and
// the one that sources through FromGoReassembled must not.
const dynSprintf = "package db\n\nimport \"fmt\"\n\nfunc load(v string) string {\n" +
	"\treturn fmt.Sprintf(\"SELECT id FROM t WHERE s = %s FOR UPDATE\", v)\n}\n"

func TestCensusSitesFollowTheRuleTypesOwnExtractor(t *testing.T) {
	cases := []struct {
		ruleType string
		want     int
	}{
		// sql/parses really does skip a Sprintf-composed candidate (it sources
		// via FromGo and drops the Partial fragments), so the census line is
		// true for it. This is the control: without it, "report nothing" passes
		// the assertion below.
		{"sql/parses", 1},
		// Both locking types resolve the very same composition into fw_expr
		// text and check it — `formwork check` fires on this file — so a census
		// line about it is a false claim.
		{"sql/locking-select-order", 0},
		{"sql/locking-target", 0},
	}
	for _, c := range cases {
		t.Run(c.ruleType, func(t *testing.T) {
			sites, ok, err := sqlparse.CensusSites(c.ruleType, "q.go", []byte(dynSprintf))
			if err != nil {
				t.Fatalf("CensusSites: %v", err)
			}
			if !ok {
				t.Fatalf("%s is a registered SQL rule type and the census must be "+
					"able to source for it", c.ruleType)
			}
			if len(sites) != c.want {
				t.Fatalf("%s: %d census site(s), want %d: %+v — a rule's coverage "+
					"gap is whatever ITS extractor could not read, never another "+
					"one's", c.ruleType, len(sites), c.want, sites)
			}
		})
	}
}

// The list, against the registry. #75 read the type-to-extractor mapping off the
// type NAME, so a new sql/* type inherited FromGo's answer on the day it
// registered whether it sources through FromGo or not — which is how two
// locking rules came to be credited with FromGo's limits. A list only fixes
// that while it stays complete, and nothing but this test can say so: the
// registry is where types actually appear.
func TestEverySQLRuleTypeNamesTheExtractorItSourcesThrough(t *testing.T) {
	seen := 0
	for _, name := range rules.TypeNames() {
		if !strings.HasPrefix(name, "sql/") {
			continue
		}
		seen++
		if !sqlparse.AccountedForByTheCensus(name) {
			t.Errorf("%s is registered and neither of unreadable.go's two lists "+
				"names it, so the census would source for it by falling through to "+
				"FromGo — the guess that made the #311 false claim", name)
		}
	}
	if seen < 4 {
		t.Fatalf("only %d sql/* types registered; the blank imports that register "+
			"them are what this test reads, so a missing one makes it vacuous", seen)
	}
}

// A rule type this package does not own must not silently get somebody else's
// answer. The prefix match that made `sql/` mean "sources through FromGo" is
// exactly how the false claim shipped.
func TestCensusSitesRefusesANonSQLRuleType(t *testing.T) {
	if _, ok, _ := sqlparse.CensusSites("forbidden-pattern", "q.go", []byte(dynSprintf)); ok {
		t.Fatal("a forbidden-pattern rule has no opinion about SQL, and crediting " +
			"it with declining to analyse some would tell an operator a rule " +
			"declined something it was never analysing")
	}
}

// AccountedForByTheCensus's property, ASSERTED AT RUNTIME AND NOT ONLY IN A
// TEST. TestEverySQLRuleTypeNamesTheExtractorItSourcesThrough reads the live
// registry and fails on a registered sql/* type neither table names, which
// stops one landing — but only in a build that runs the tests. In the binary,
// such a type still fell through to sqlextract.FromGo and got its answer
// printed under the new rule's id, which is the guess #311 was filed about,
// arriving by the same route: a mapping read off the type NAME rather than
// from the rule's own sourcing.
//
// So the fall-through is refused, with an error a caller can tell from a Go
// parse failure. A file that does not parse is the rule's to report and a
// caller is right to skip it; a type the census cannot source for is the
// census's own gap and has to be said out loud, because skipping it reports
// that rule's coverage as clean.
func TestCensusSitesRefusesASQLTypeNeitherTableNames(t *testing.T) {
	sites, ok, err := sqlparse.CensusSites("sql/not-yet-taught", "q.go", []byte(dynSprintf))
	if !ok {
		t.Fatal("a sql/* type is a SQL rule whether or not the census can source " +
			"for it; ok=false says the caller asked about the wrong kind of rule")
	}
	if err == nil {
		t.Fatalf("a sql/* type in neither table got an answer anyway (%d site(s): "+
			"%+v) — falling through to FromGo is the guess #311 was filed about",
			len(sites), sites)
	}
	if !errors.Is(err, sqlparse.ErrExtractorUnknown) {
		t.Fatalf("the error must be distinguishable from a Go parse failure, "+
			"which a caller is right to skip: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("refusing and answering at the same time invites a caller to "+
			"print the answer: %+v", sites)
	}
}

// The narrowing, and without it the refusal above is satisfied by refusing
// everything. Each registered type still gets its own extractor's answer.
func TestCensusSitesStillAnswersForEveryRegisteredType(t *testing.T) {
	for _, name := range rules.TypeNames() {
		if !strings.HasPrefix(name, "sql/") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if _, ok, err := sqlparse.CensusSites(name, "q.go", []byte(dynSprintf)); err != nil || !ok {
				t.Fatalf("a registered sql/* type must be sourced, not refused: ok=%v err=%v", ok, err)
			}
		})
	}
}

// WHERE the site sits, and this is not cosmetic. The expression walk emits every
// seed literal whatever the fold does, so a seed that is itself an unordered
// locking SELECT FIRES at its own line. A site anchored there would say "not
// analysed" about a line the same run just failed on — #311's defect, rebuilt by
// its own fix. The site belongs at the write that could not be read.
func TestUnreadableSiteIsAnchoredAtTheWriteNotTheSeed(t *testing.T) {
	src := "package db\n\nfunc load() string {\n" +
		"\tq := \"SELECT id FROM t FOR UPDATE\"\n" + // line 4: fires on its own
		"\torderIt(&q)\n" + // line 5: the write nothing here can read
		"\treturn q\n}\n"
	// Precondition: the rule really does fire on the seed line, so the two
	// claims would collide if the site were anchored there.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("q.go", src))
	if len(ms) != 1 || ms[0].Line != 4 {
		t.Fatalf("precondition: the seed literal should fire at line 4, got %+v", ms)
	}
	got := sitesFor(t, src)
	if len(got) != 1 {
		t.Fatalf("one escaping write, one site, got %+v", got)
	}
	if got[0].Line != 5 {
		t.Fatalf("site anchored at line %d; the escaping `&q` is at line 5 and the "+
			"seed the rule DID analyse is at line 4. Anchoring at the seed prints "+
			"\"not analysed\" about the line `formwork check` just failed on.",
			got[0].Line)
	}
}

// The filter. The fold tracks every string-literal-seeded local, and an
// operator's repo is mostly paths, messages and format strings. Reporting each
// one buries the SQL gate's real gaps under the whole tree.
func TestUnreadableSitesSkipCompositionsThatAreNotSQL(t *testing.T) {
	src := "package db\n\nfunc load() string {\n" +
		"\tmsg := \"could not reach the pallet service\"\n" +
		"\torderIt(&msg)\n" +
		"\treturn msg\n}\n"
	if got := sitesFor(t, src); len(got) != 0 {
		t.Fatalf("an untracked non-SQL string is not a coverage gap in a SQL "+
			"rule: %+v", got)
	}
}

// The reason table and the corpus have to agree, or a reason is a struct with
// prose around it — the #313 failure, pointed at the new list.
func TestEveryUnreadableReasonIsProducedBySomeDisclosedShape(t *testing.T) {
	reasons := sqlextract.UnreadableReasons()
	if len(reasons) <= len(sqlextract.UntrackReasons()) {
		t.Fatal("UnreadableReasons must be a strict superset of UntrackReasons — " +
			"the compositions the fold declines WITHOUT untracking are the half " +
			"#311 is about")
	}
	seen := map[string]bool{}
	for key, src := range covShapes {
		for _, s := range sitesFor(t, src) {
			seen[s.Key] = true
			_ = key
		}
	}
	for _, r := range reasons {
		if !seen[r.Key] {
			t.Errorf("sqlextract lists %q as a reason a composition goes unread, "+
				"and no disclosed shape produces it — either the classifier "+
				"stopped stamping it or the entry is prose with a struct around it",
				r.Key)
		}
	}
}

// And the other way: a site can only ever carry a reason the table lists, or an
// operator reads a refusal no disclosure covers.
func TestEveryEmittedSiteCarriesAListedReason(t *testing.T) {
	listed := map[string]string{}
	for _, r := range sqlextract.UnreadableReasons() {
		listed[r.Key] = r.Detail
	}
	emitted := 0
	for key, src := range covShapes {
		for _, s := range sitesFor(t, src) {
			emitted++
			detail, ok := listed[s.Key]
			if !ok {
				t.Fatalf("shape %q emitted key %q, which UnreadableReasons does not "+
					"list", key, s.Key)
			}
			if s.Reason != detail {
				t.Fatalf("shape %q emitted reason %q for key %q; the table says %q",
					key, s.Reason, s.Key, detail)
			}
		}
	}
	if emitted == 0 {
		t.Fatal("no site emitted by any shape — the assertions above are vacuous")
	}
}

// The issue's own reproduction, at the seam the census calls.
//
// #311's second half was demonstrated with four files, each hiding an unordered
// locking SELECT behind a composition the fold cannot read: a strings.Builder, a
// loop, a called named closure, and an address handed to a helper. `formwork
// check` reported "[lock-order] OK, 0 findings, exit 0" and `formwork lint`
// listed nothing but the declared fixture_exempt — the gate silent, and the
// channel built to disclose that silence silent with it.
//
// Kept as its own test beside the corpus-driven ones because it is the artifact
// the issue was filed on: the corpus proves the invariant, and this proves the
// invariant covers the case somebody actually hit.
func TestTheFourFileReproductionIsNoLongerSilent(t *testing.T) {
	const seed = "SELECT id FROM t WHERE s = 'x'"
	files := map[string]string{
		"builder.go": "package db\n\nimport \"strings\"\n\nfunc Q() string {\n" +
			"\tvar b strings.Builder\n\tb.WriteString(\"" + seed + "\")\n" +
			"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n}\n",
		"loop.go": "package db\n\nfunc L(parts []string) string {\n" +
			"\tq := \"" + seed + "\"\n\tfor range parts {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
			"\treturn q\n}\n",
		"closure.go": "package db\n\nfunc C() string {\n\tq := \"" + seed + "\"\n" +
			"\tadd := func() { q += \" FOR UPDATE\" }\n\tadd()\n\treturn q\n}\n",
		"escape.go": "package db\n\nfunc lockIt(p *string) { *p += \" FOR UPDATE\" }\n\n" +
			"func E() string {\n\tq := \"" + seed + "\"\n\tlockIt(&q)\n\treturn q\n}\n",
	}
	for name, src := range files {
		t.Run(name, func(t *testing.T) {
			// The gate really is silent on it — that is the premise, and without
			// it the assertion below is about a file the rule would have caught.
			c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
			if ms := matches(t, c, file(name, src)); len(ms) != 0 {
				t.Fatalf("premise: the rule is supposed to report nothing here: %+v", ms)
			}
			sites, ok, err := sqlparse.CensusSites("sql/locking-select-order", name, []byte(src))
			if err != nil || !ok {
				t.Fatalf("CensusSites: ok=%v err=%v", ok, err)
			}
			if len(sites) != 1 {
				t.Fatalf("this file hides an unordered locking SELECT behind a "+
					"composition the rule cannot read, and the census reports %d "+
					"line(s) about it: %+v", len(sites), sites)
			}
		})
	}
}

// THE BUILDER SHAPE THE FOUR-FILE REPRO ACTUALLY USED, and it stayed silent
// through #311's first fix. TestTheFourFileReproductionIsNoLongerSilent seeds
// its builder from a literal written straight into it
// (`b.WriteString("SELECT …")`). The reviewer's repo — and the refutation
// record's, twice over — seeds a LOCAL with the benign query and writes the
// local in: `q := "SELECT id FROM t WHERE s = 'x'"; b.WriteString(q);
// b.WriteString(" FOR UPDATE")`.
//
// That one difference took the census back to zero. builderSites contributes
// text only from an argument reassembleOperand can read, and a bare identifier
// is not one, so the builder accumulated " FOR UPDATE" alone — which
// looksLikeSQL correctly judges not to be SQL, and UnreadableSites correctly
// dropped. The rule reports nothing about the file either, because the only
// name it tracks holds the unlocked seed. Silent gate, silent census: #311
// half 2 verbatim, one shape smaller than the shape its own fix pinned.
//
// The seed is text that went INTO the builder, so the builder's text has to
// carry it. Every row is a way an operator spells that, and each is here
// because it reaches a distinct arm of the resolution — a bare name, a name
// inside a `+`, a parenthesised name, a name bound by `var` and then by `=`,
// and two shapes whose left-hand side is not a plain name at all. The
// direct-literal row rides along so the two spellings cannot drift: the
// census's answer must not depend on whether the operator inlined their seed.
func TestBuilderWrittenFromALiteralSeededLocalIsNotSilent(t *testing.T) {
	const seed = "SELECT id FROM t WHERE s = 'x'"
	const decl = "package db\n\nimport \"strings\"\n\nfunc two() (string, string) { return \"a\", \"b\" }\n\nfunc Q(m map[string]string) string {\n"
	for _, tc := range []struct {
		name string
		body string
	}{
		// A bare identifier: the shape the reviewer's repo used.
		{"bare-ident", "\tq := \"" + seed + "\"\n\tvar b strings.Builder\n" +
			"\tb.WriteString(q)\n\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// Bound by a var declaration rather than by :=.
		{"var-decl", "\tvar q = \"" + seed + "\"\n\tvar b strings.Builder\n" +
			"\tb.WriteString(q)\n\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// Declared, then assigned: the binding is a plain `=`, not a `:=`, and
		// the declaration carries no value to read.
		{"plain-assign", "\tvar q string\n\tq = \"" + seed + "\"\n\tvar b strings.Builder\n" +
			"\tb.WriteString(q)\n\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// The name inside a `+`, so the whole write is one expression.
		{"ident-in-concat", "\tq := \"" + seed + "\"\n\tvar b strings.Builder\n" +
			"\tb.WriteString(q + \" FOR UPDATE\")\n\treturn b.String()\n"},
		// Parenthesised, which reassembleOperand unwraps for a literal and
		// which has to be unwrapped for a name too.
		{"parenthesised-ident", "\tq := \"" + seed + "\"\n\tvar b strings.Builder\n" +
			"\tb.WriteString((q))\n\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// Bound twice. Both bindings are SQL, and an operator who cannot tell
		// which one reaches the builder is exactly who the census line is for,
		// so neither binding may swallow the other into silence.
		{"rebound", "\tq := \"" + seed + "\"\n\tq = \"SELECT id FROM t WHERE s = 'y'\"\n" +
			"\tvar b strings.Builder\n\tb.WriteString(q)\n" +
			"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// A multi-value `:=` in the same body: one right-hand expression for
		// two names, which pairs by index with nothing.
		{"alongside-multi-value-assign", "\tx, y := two()\n\t_, _ = x, y\n\tq := \"" + seed + "\"\n" +
			"\tvar b strings.Builder\n\tb.WriteString(q)\n" +
			"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// A left-hand side that is not a name at all.
		{"alongside-index-assign", "\tm[\"k\"] = \"SELECT id FROM u\"\n\tq := \"" + seed + "\"\n" +
			"\tvar b strings.Builder\n\tb.WriteString(q)\n" +
			"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
		// Bound twice with the query SPLIT ACROSS the bindings, so no single
		// one of them is SQL-shaped on its own: the first has no structural
		// keyword after its SELECT, and the second does not lead with a
		// statement keyword at all. A name bound more than once holds text this
		// pass cannot choose between, and choosing either would answer "is any
		// SQL flowing into this builder" with silence.
		{"split-across-bindings", "\tq := \"SELECT id\"\n\tq = \" FROM t WHERE s = 'x' FOR UPDATE\"\n" +
			"\tvar b strings.Builder\n\tb.WriteString(q)\n\treturn b.String()\n"},
		// The spelling that already worked, so the two cannot drift apart.
		{"direct-literal", "\tvar b strings.Builder\n\tb.WriteString(\"" + seed + "\")\n" +
			"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := decl + tc.body + "}\n"
			// The premise, and without it this asserts about a file the rule
			// would have caught anyway.
			c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
			if ms := matches(t, c, file("b.go", src)); len(ms) != 0 {
				t.Fatalf("premise: the rule is supposed to report nothing here: %+v", ms)
			}
			sites, ok, err := sqlparse.CensusSites("sql/locking-select-order", "b.go", []byte(src))
			if err != nil || !ok {
				t.Fatalf("CensusSites: ok=%v err=%v", ok, err)
			}
			if len(sites) != 1 {
				t.Fatalf("this file hides an unordered locking SELECT in a builder "+
					"the fold never seeds, and the census reports %d line(s) about "+
					"it: %+v", len(sites), sites)
			}
		})
	}
}

// The narrowing, and it is the one that keeps the fix from becoming the flood
// builder.go's doc warns about: over-recognising would put a census line
// against every .WriteString in the repo, which is what makes a diagnostic
// channel unreadable. Resolving an identifier must not report a builder
// assembling a path or a log line merely because a local was involved, and a
// name this pass cannot read must still contribute nothing rather than a
// placeholder that reads as text.
func TestBuilderWrittenFromANonSQLLocalStaysUnreported(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"path-local", "\tp := \"/var/log/app\"\n\tvar b strings.Builder\n" +
			"\tb.WriteString(p)\n\tb.WriteString(\"/today.log\")\n\treturn b.String()\n"},
		{"unreadable-name", "\tvar b strings.Builder\n\tb.WriteString(runtimeQuery())\n" +
			"\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package db\n\nimport \"strings\"\n\nfunc runtimeQuery() string { return \"\" }\n\nfunc Q() string {\n" + tc.body + "}\n"
			sites, ok, err := sqlparse.CensusSites("sql/locking-select-order", "b.go", []byte(src))
			if err != nil || !ok {
				t.Fatalf("CensusSites: ok=%v err=%v", ok, err)
			}
			if len(sites) != 0 {
				t.Fatalf("this builder is not a SQL coverage gap, and a line about "+
					"it buries the ones that are: %+v", sites)
			}
		})
	}
}
