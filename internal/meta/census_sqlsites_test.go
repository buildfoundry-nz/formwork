// census_sqlsites_test.go — #75.
//
// sqlextract.FromGo returns three values: resolved candidates, unresolved
// Sites, and an error. A Site is the honest record of "there is SQL here and I
// could not read it". Both consumers wrote `cands, _, err :=` and threw it away,
// so every composition the extractor cannot model disappeared with no finding,
// no diagnostic, and nothing for lint to count.
//
// Spec §9's coverage limits — strings.Builder, loop/switch/select composition,
// a named closure's appends, goto, writes through a taken address — were all
// disclosed only as PROSE in a doc comment, discoverable by someone who already
// suspected the gap. There was no way to ask "how many queries in this repo did
// the gate decline to analyse" and get a number.
//
// That is the one exemption channel the census did not cover, and #55 is the
// precedent: it made scope.exclude countable rather than leaving it silent.
//
// FAILING CLOSED MOVED TWO TYPES OUT OF IT. sql/parses and
// sql/statement-predicate now report each SQL composition they cannot read as a
// finding, so for them nothing unread is left unenforced and the census owes
// them no line. The locking pair still reads compositions through placeholder
// reassembly and still has compositions it declines (builders, loops), so the
// census lists those, and the tests below that pin the census's narrowing are
// written against a locking rule.

package meta_test

import (
	"strings"
	"testing"
)

const sqlRule = "rules:\n" +
	"  - id: sql-locking\n" +
	"    type: sql/locking-select-order\n" +
	"    scope: {include: ['**/*.go']}\n" +
	"    params: {unique_key_columns: [id]}\n" +
	"    fixture_exempt: \"parse-tree rule; the corpus is the fixture\"\n"

// builderSQL is a composition the locking rules decline to read: a
// strings.Builder written from a literal-seeded local (sqlextract's sites.go).
const builderSQL = "package p\n\nimport \"strings\"\n\nfunc q() string {\n" +
	"\ts := \"SELECT id FROM t WHERE s = 'x'\"\n\tvar b strings.Builder\n" +
	"\tb.WriteString(s)\n\tb.WriteString(\" FOR UPDATE\")\n\treturn b.String()\n}\n"

func sqlRepo(extra map[string]string) map[string]string {
	base := map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  sqlRule,
		"clean.go":                "package p\n\nconst q = `SELECT id FROM t`\n",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// The signal: a composition the extractor cannot reassemble is named, with the
// reason, rather than vanishing.
func TestCensusReportsUnanalysableSQL(t *testing.T) {
	_, out := lint(t, sqlRepo(map[string]string{"dyn.go": builderSQL}))
	if !strings.Contains(out, "sql-locking: SQL at dyn.go") {
		t.Fatalf("an unreadable SQL composition must be named:\n%s", out)
	}
	if !strings.Contains(out, "could not be read") {
		t.Fatalf("the census must say the extractor could not read it:\n%s", out)
	}
}

// The narrowing. A file the extractor reads completely is not an exemption, and
// reporting it would bury the signal under every SQL literal in the repo.
func TestCensusDoesNotReportAnalysableSQL(t *testing.T) {
	_, out := lint(t, sqlRepo(nil))
	if strings.Contains(out, "clean.go") {
		t.Fatalf("a fully-read composition is not an exemption:\n%s", out)
	}
}

// Only files a SQL rule actually governs. A Go file no SQL rule scopes is not a
// coverage gap in the SQL gate, and scanning the whole tree would report
// compositions nothing was ever going to analyse.
func TestCensusIgnoresGoFilesNoSQLRuleGoverns(t *testing.T) {
	files := sqlRepo(map[string]string{"other/dyn.go": builderSQL, "dyn.go": builderSQL})
	files[".formwork/rules/r.yaml"] = strings.Replace(sqlRule,
		"include: ['**/*.go']", "include: ['dyn.go']", 1)
	_, out := lint(t, files)
	if !strings.Contains(out, "SQL at dyn.go") {
		t.Fatalf("precondition: the governed file is reported:\n%s", out)
	}
	if strings.Contains(out, "other/dyn.go") {
		t.Fatalf("a file no SQL rule governs is out of scope:\n%s", out)
	}
}

// The narrowing isSQLRule provides, and it needed its own test: with only a SQL
// rule in the fixture, widening isSQLRule to "every rule" changed no output and
// the mutation survived. A forbidden-pattern rule governing the same Go file has
// no opinion about SQL, so an unreadable composition is not a gap in ITS
// coverage — reporting it under that rule's name would tell the operator a rule
// declined to analyse something it was never analysing.
func TestCensusDoesNotAttributeSQLSitesToANonSQLRule(t *testing.T) {
	files := sqlRepo(map[string]string{"dyn.go": builderSQL})
	files[".formwork/rules/r.yaml"] = sqlRule +
		"  - id: no-ghost\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    params: {pattern: 'Ghost'}\n" +
		"    cure: \"drop it\"\n"
	files[".formwork/fixtures/no-ghost/fire-1/a.go"] = "package p // Ghost want: no-ghost\n"
	files[".formwork/fixtures/no-ghost/pass-1/b.go"] = "package p\n"

	_, out := lint(t, files)
	if !strings.Contains(out, "sql-locking: SQL at dyn.go") {
		t.Fatalf("precondition: the SQL rule should report it:\n%s", out)
	}
	if strings.Contains(out, "no-ghost: SQL at") {
		t.Fatalf("a non-SQL rule must not be credited with declining to analyse SQL:\n%s", out)
	}
}

// A rule that fails closed has no escape hatch to list. sql/parses reports the
// composition it cannot read as a finding (sqlparse's statementsWithSites), so
// a census line calling the same composition "not analysed by this rule" would
// contradict the check that just failed on it.
func TestCensusListsNothingForARuleThatFailsClosed(t *testing.T) {
	files := sqlRepo(map[string]string{
		"dyn.go": "package p\n\nimport \"fmt\"\n\nfunc q(t string) string {\n" +
			"\treturn fmt.Sprintf(\"SELECT id FROM %s WHERE s = 'x'\", t)\n}\n",
	})
	files[".formwork/rules/r.yaml"] = "rules:\n" +
		"  - id: sql-parses\n" +
		"    type: sql/parses\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    params: {}\n" +
		"    fixture_exempt: \"parse-tree rule; the corpus is the fixture\"\n"
	_, out := lint(t, files)
	if strings.Contains(out, "sql-parses: SQL at") {
		t.Fatalf("sql/parses fails on what it cannot read; the census owes it no line:\n%s", out)
	}
}
