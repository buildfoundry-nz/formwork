package sqlparse_test

import (
	"strconv"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Content) == 0 {
		return &doc
	}
	return doc.Content[0]
}

func checker(t *testing.T, typ, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup(typ)
	if !ok {
		t.Fatalf("type %q not registered", typ)
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	return c
}

func file(name, body string) *scan.File { return scan.NewMemFile(name, []byte(body)) }

func getFactory(t *testing.T, typ string) (func(*yaml.Node) (rules.Checker, error), bool) {
	t.Helper()
	return rules.Lookup(typ)
}

func matches(t *testing.T, c rules.Checker, f *scan.File) []rules.Match {
	t.Helper()
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatalf("CheckFile(%s): %v", f.Path(), err)
	}
	return ms
}

func TestParsesValidSQLPasses(t *testing.T) {
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("q.sql", "SELECT * FROM t WHERE id = $1;\n")); len(ms) != 0 {
		t.Fatalf("valid SQL must pass: %+v", ms)
	}
}

func TestParsesMalformedSQLFires(t *testing.T) {
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("q.sql", "SELECT FROM;\n")); len(ms) != 1 {
		t.Fatalf("malformed SQL must fire once: %+v", ms)
	}
}

func TestParsesSemicolonInStringIsClean(t *testing.T) {
	// [R1] no naive-split false positive.
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("q.sql", "INSERT INTO x(msg) VALUES('a; b');\n")); len(ms) != 0 {
		t.Fatalf("';' inside a string literal must not fire: %+v", ms)
	}
}

func TestParsesIgnoresNonSQLGoLiterals(t *testing.T) {
	// [R2] import path + struct tag + log string must NOT fire.
	src := "package db\n\nimport \"fmt\"\n\ntype T struct{ A int `json:\"a\"` }\n\nfunc f() { _ = fmt.Sprint(\"hello world\") }\n"
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
		t.Fatalf("non-SQL Go literals must not fire: %+v", ms)
	}
}

func TestParsesFiresOnMalformedSQLShapedGoLiteral(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT FROM WHERE\" }\n"
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("db.go", src)); len(ms) != 1 {
		t.Fatalf("a SQL-shaped literal that doesn't parse must fire: %+v", ms)
	}
}

func TestParsesIgnoresProseStartingWithSQLKeyword(t *testing.T) {
	// [0] a leading SQL keyword alone must not be enough; prose lacks a
	// second structural keyword and must not fire.
	c := checker(t, "sql/parses", "")
	src := "package db\n\nfunc a() string { return \"SELECT a plan to continue\" }\nfunc b() string { return \"UPDATE your profile settings\" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
		t.Fatalf("prose starting with a SQL keyword must not fire: %+v", ms)
	}
}

func TestParsesIgnoresLoneFragmentWithoutStructuralKeyword(t *testing.T) {
	// [6] a strings.Builder-style lone fragment with no second structural
	// keyword (no FROM/WHERE/etc.) must not fire.
	c := checker(t, "sql/parses", "")
	src := "package db\n\nfunc q() string { return \"SELECT id, \" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
		t.Fatalf("a lone SQL fragment without a structural keyword must not fire: %+v", ms)
	}
}

func TestParsesFiresOnExpandedLeadingKeywordWithStructuralKeyword(t *testing.T) {
	// [8] EXPLAIN is now in the leading-keyword set, and this literal also
	// has a structural WHERE, so it must still fire despite the malformed FRM.
	src := "package db\n\nfunc q() string { return \"EXPLAIN SELECT x FRM t WHERE y\" }\n"
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("db.go", src)); len(ms) != 1 {
		t.Fatalf("an expanded-keyword SQL-shaped literal with a structural keyword must fire: %+v", ms)
	}
}

func TestParsesDegenerateLeadingParenNoPanic(t *testing.T) {
	// looksLikeSQL must not panic on minimal/degenerate input.
	c := checker(t, "sql/parses", "")
	for _, body := range []string{"(", "()", "-- x"} {
		src := "package db\n\nfunc q() string { return " + strconv.Quote(body) + " }\n"
		if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
			t.Fatalf("degenerate literal %q must not be flagged: %+v", body, ms)
		}
	}
}

func TestParsesStillIgnoresConcatenatedGoQuery(t *testing.T) {
	// Fix #12 regression guard: sql/parses must stay on statements() (strict,
	// Partial-skipped) — a concatenated query is still an unresolved
	// composition here, unaffected by sql/locking-select-order's new
	// best-effort reassembly path.
	src := "package db\n\nfunc q(tbl string) string {\n\treturn \"SELECT * FROM \" + tbl + \" WHERE status='x' FOR UPDATE\"\n}\n"
	c := checker(t, "sql/parses", "")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("a concatenated query must still be Partial-skipped by sql/parses: %+v", ms)
	}
}

// Fix: everyday English words (SET, SHOW, COMMENT, ANALYZE, TABLE, VALUES) are
// too weak as statement-initial markers — combined with the common-English
// structural words (ON/AS/ORDER/FROM) they make ordinary prose literals parse
// as SQL and fire.

func TestParsesIgnoresProseStartingWithAmbiguousKeyword(t *testing.T) {
	c := checker(t, "sql/parses", "")
	for _, prose := range []string{
		"Set the order to pending",
		"Comment on the record as needed",
		"Show the values from the archive",
		"Analyze the results from last week",
		"Table of contents as rendered",
		"Values from the archive on disk",
	} {
		src := "package db\n\nfunc q() string { return " + strconv.Quote(prose) + " }\n"
		if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
			t.Fatalf("prose %q must not be reported as unparseable SQL: %+v", prose, ms)
		}
	}
}

// Fix: short DDL has no structural keyword after its leading token, so the
// second-keyword requirement silently dropped it from .go coverage. A DDL
// object keyword immediately after CREATE/ALTER/DROP/TRUNCATE is the SQL
// signal instead.

func TestParsesFiresOnMalformedCreateTableInGoLiteral(t *testing.T) {
	c := checker(t, "sql/parses", "")
	src := "package db\n\nfunc q() string { return \"CREATE TABLE users (id uuid, email txt NOT NULL,)\" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 1 {
		t.Fatalf("malformed CREATE TABLE must fire: %+v", ms)
	}
}

func TestParsesFiresOnMalformedDropTableInGoLiteral(t *testing.T) {
	c := checker(t, "sql/parses", "")
	src := "package db\n\nfunc q() string { return \"DROP TABLE IF EXITS temp_rows\" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 1 {
		t.Fatalf("malformed DROP TABLE must fire: %+v", ms)
	}
}

func TestParsesValidCreateIndexInGoLiteralPasses(t *testing.T) {
	c := checker(t, "sql/parses", "")
	src := "package db\n\nfunc q() string { return \"CREATE UNIQUE INDEX idx ON t (id)\" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
		t.Fatalf("valid DDL must pass: %+v", ms)
	}
}

func TestParsesIgnoresProseStartingWithDDLKeyword(t *testing.T) {
	// The DDL arm keys on the object keyword adjacent to the leading token
	// (modulo DDL modifiers), so prose that merely starts with CREATE/DROP is
	// not rescued by it. NOTE: prose that ALSO contains a structural keyword
	// ("…as soon as you can") still fires through the structural arm — a
	// pre-existing residual of ON/AS being real SQL keywords, documented on
	// looksLikeSQL and deliberately not addressed here.
	c := checker(t, "sql/parses", "")
	for _, prose := range []string{
		"Drop the file quietly",
		"Create a backup copy now",
		"Alter the plan before Friday",
	} {
		src := "package db\n\nfunc q() string { return " + strconv.Quote(prose) + " }\n"
		if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
			t.Fatalf("prose %q must not fire: %+v", prose, ms)
		}
	}
}

func TestParsesRejectsUnknownParam(t *testing.T) {
	factory, _ := rules.Lookup("sql/parses")
	if _, err := factory(paramsNode(t, "bogus: true\n")); err == nil {
		t.Fatal("unknown param must be rejected (strict decode)")
	}
}
