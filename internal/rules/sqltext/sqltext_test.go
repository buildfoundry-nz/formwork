package sqltext_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
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

func mustChecker(t *testing.T, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup("sql/statement-predicate")
	if !ok {
		t.Fatal(`type "sql/statement-predicate" not registered`)
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	return c
}

func file(name, body string) *scan.File { return scan.NewMemFile(name, []byte(body)) }

func check(t *testing.T, c rules.Checker, f *scan.File) []rules.Match {
	t.Helper()
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatalf("CheckFile(%s) error: %v", f.Path(), err)
	}
	return ms
}

// --- .sql fixtures ---

func TestSQLMissingRequiredTokenViolates(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	ms := check(t, c, file("q.sql", "SELECT * FROM users;\n"))
	if len(ms) != 1 {
		t.Fatalf("statement on users without WHERE must violate: %+v", ms)
	}
	if ms[0].Line != 1 {
		t.Fatalf("want line 1, got %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "WHERE") {
		t.Fatalf("message must name the missing token: %q", ms[0].Message)
	}
}

func TestSQLRequiredTokenSatisfiedPasses(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	ms := check(t, c, file("q.sql", "SELECT * FROM users WHERE id = 1;\n"))
	if len(ms) != 0 {
		t.Fatalf("statement satisfying require must pass: %+v", ms)
	}
}

func TestSQLForbiddenTokenViolates(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nforbid:\n  - 'SELECT \\*'\n")
	ms := check(t, c, file("q.sql", "SELECT * FROM users;\n"))
	if len(ms) != 1 {
		t.Fatalf("forbidden SELECT * must violate: %+v", ms)
	}
	if !strings.Contains(strings.ToLower(ms[0].Message), "forbid") {
		t.Fatalf("message must mention forbidden: %q", ms[0].Message)
	}
}

func TestSQLNonMatchingTableIgnored(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	// mentions orders, not users; require WHERE absent but table doesn't match.
	ms := check(t, c, file("q.sql", "SELECT * FROM orders;\n"))
	if len(ms) != 0 {
		t.Fatalf("statement not mentioning the table must be ignored: %+v", ms)
	}
}

func TestSQLOnlyViolatingStatementReported(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	body := "SELECT * FROM users WHERE id=1;\n" + // ok
		"SELECT * FROM orders;\n" + // not the table
		"DELETE FROM users;\n" // violates: no WHERE
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("only the violating users statement should report: %+v", ms)
	}
	if ms[0].Line != 3 {
		t.Fatalf("want violating statement on line 3, got %d", ms[0].Line)
	}
}

// --- .go fixtures (concatenated string-literal reassembly) ---

func TestGoConcatReassemblyForbidViolates(t *testing.T) {
	src := "package db\n" +
		"\n" +
		"const createUsers = \"CREATE TABLE users (\" +\n" +
		"\t\"id \" +\n" +
		"\t\"int PRIMARY KEY)\"\n"
	c := mustChecker(t, "table: 'users'\nforbid:\n  - 'PRIMARY KEY'\n")
	ms := check(t, c, file("db.go", src))
	if len(ms) != 1 {
		t.Fatalf("reassembled CREATE TABLE with forbidden PRIMARY KEY must violate: %+v", ms)
	}
	if ms[0].Line == 0 {
		t.Fatalf("go finding must carry a line number: %+v", ms[0])
	}
}

func TestGoConcatReassemblySatisfiesRequireAcrossBoundary(t *testing.T) {
	// "id int" only exists once the "id " and "int..." literals are joined.
	src := "package db\n" +
		"\n" +
		"const createUsers = \"CREATE TABLE users (\" +\n" +
		"\t\"id \" +\n" +
		"\t\"int PRIMARY KEY)\"\n"
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'id int'\n")
	ms := check(t, c, file("db.go", src))
	if len(ms) != 0 {
		t.Fatalf("require spanning the concat boundary must be satisfied: %+v", ms)
	}
}

func TestGoSingleLiteralForbid(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT * FROM orders;\" }\n"
	c := mustChecker(t, "table: 'orders'\nforbid:\n  - 'SELECT'\n")
	ms := check(t, c, file("db.go", src))
	if len(ms) != 1 {
		t.Fatalf("standalone literal with forbidden token must violate: %+v", ms)
	}
}

func TestGoNoSQLNoFindings(t *testing.T) {
	src := "package db\n\nvar greeting = \"hello world\"\n"
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	ms := check(t, c, file("db.go", src))
	if len(ms) != 0 {
		t.Fatalf("no matching table means no findings: %+v", ms)
	}
}

func TestGoParseFailureIsError(t *testing.T) {
	src := "package db\n\nfunc broken( {\n"
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	if _, err := c.CheckFile(file("bad.go", src)); err == nil {
		t.Fatal("a .go file that fails to parse must return an error, not pass")
	}
}

// --- non-target files ---

func TestNonTargetExtensionSkipped(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nforbid:\n  - 'x'\n")
	for _, name := range []string{"README.md", "data.txt", "config.yaml"} {
		ms := check(t, c, file(name, "SELECT * FROM users;\n"))
		if ms != nil {
			t.Fatalf("%s: non .go/.sql file must yield nil findings: %+v", name, ms)
		}
	}
}

// --- param validation (exit-2 config errors) ---

func TestRejectsMissingTable(t *testing.T) {
	factory, _ := rules.Lookup("sql/statement-predicate")
	if _, err := factory(paramsNode(t, "require:\n  - 'x'\n")); err == nil {
		t.Fatal("missing table accepted")
	}
}

func TestRejectsNeitherRequireNorForbid(t *testing.T) {
	factory, _ := rules.Lookup("sql/statement-predicate")
	if _, err := factory(paramsNode(t, "table: 'users'\n")); err == nil {
		t.Fatal("neither require nor forbid accepted")
	}
}

func TestRejectsBadRegex(t *testing.T) {
	factory, _ := rules.Lookup("sql/statement-predicate")
	if _, err := factory(paramsNode(t, "table: '('\nrequire:\n  - 'x'\n")); err == nil {
		t.Fatal("invalid table regex accepted")
	}
	if _, err := factory(paramsNode(t, "table: 'users'\nrequire:\n  - '('\n")); err == nil {
		t.Fatal("invalid require regex accepted")
	}
	if _, err := factory(paramsNode(t, "table: 'users'\nforbid:\n  - '('\n")); err == nil {
		t.Fatal("invalid forbid regex accepted")
	}
}

func TestRejectsUnknownField(t *testing.T) {
	factory, _ := rules.Lookup("sql/statement-predicate")
	if _, err := factory(paramsNode(t, "table: 'users'\nrequire:\n  - 'x'\nbogus: true\n")); err == nil {
		t.Fatal("unknown param field accepted")
	}
}

func TestGoMultiStatementLiteralCollapsesToCandidateLine(t *testing.T) {
	// One backtick literal holding two statements; the 2nd must still report
	// the candidate's start line (the collapse in goStatements), not line 2.
	src := "package db\n\nconst q = `SELECT * FROM users WHERE id=1;\nDELETE FROM users;`\n"
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	ms := check(t, c, file("db.go", src))
	if len(ms) != 1 {
		t.Fatalf("only the DELETE (no WHERE) should violate: %+v", ms)
	}
	if ms[0].Line != 3 {
		t.Fatalf("want the candidate line 3 (collapse), got %d", ms[0].Line)
	}
}
