package sqlparse

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func memfile(name, body string) *scan.File { return scan.NewMemFile(name, []byte(body)) }

func TestStatementsSQLFileSplitsAndLines(t *testing.T) {
	stmts, fails, err := statements(memfile("q.sql", "SELECT 1;\nSELECT 2;\n"))
	if err != nil || len(fails) != 0 {
		t.Fatalf("clean sql: err=%v fails=%+v", err, fails)
	}
	if len(stmts) != 2 {
		t.Fatalf("want 2 stmts, got %d", len(stmts))
	}
	if stmts[1].Line != 2 {
		t.Fatalf("want 2nd stmt on line 2, got %d", stmts[1].Line)
	}
}

func TestStatementsSQLSyntaxErrorIsFailureNotErr(t *testing.T) {
	stmts, fails, err := statements(memfile("bad.sql", "SELECT FROM;\n"))
	if err != nil {
		t.Fatalf("a SQL syntax error must be a failure, not err: %v", err)
	}
	if len(stmts) != 0 || len(fails) != 1 {
		t.Fatalf("want 0 stmts / 1 failure, got %d / %d", len(stmts), len(fails))
	}
	if fails[0].FromGo {
		t.Fatal("a .sql failure is not FromGo")
	}
}

func TestStatementsGoCandidateParsedWhole(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT * FROM t FOR UPDATE\" }\n"
	stmts, fails, err := statements(memfile("db.go", src))
	if err != nil || len(fails) != 0 {
		t.Fatalf("clean go sql: err=%v fails=%+v", err, fails)
	}
	if len(stmts) != 1 {
		t.Fatalf("want 1 stmt from the literal, got %d", len(stmts))
	}
	if stmts[0].Line != 3 {
		t.Fatalf("want candidate line 3, got %d", stmts[0].Line)
	}
}

func TestStatementsGoNonSQLLiteralIsSkippedBeforeParsing(t *testing.T) {
	// import path + struct tag are returned by sqlextract as candidates, but
	// neither is SQL-shaped (looksLikeSQL). Task #10 moves that gate upstream
	// of the WASM parse: a non-SQL-shaped .go candidate is now skipped
	// entirely — never parsed, so it is neither a Stmt nor a parseFailure.
	// (Previously it was parsed, surfaced as a FromGo parseFailure, and only
	// then filtered by sql/parses.CheckFile's looksLikeSQL check; the
	// observable sql/parses findings are unchanged — see parses_test.go's
	// TestParsesIgnoresNonSQLGoLiterals.)
	src := "package db\n\nimport \"fmt\"\n\ntype T struct{ A int `json:\"a\"` }\n\nvar _ = fmt.Sprint\n"
	stmts, fails, err := statements(memfile("db.go", src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(stmts) != 0 {
		t.Fatalf("no valid SQL statements expected, got %d", len(stmts))
	}
	if len(fails) != 0 {
		t.Fatalf("non-SQL-shaped literals must be gated out before parsing, got %d failures: %+v", len(fails), fails)
	}
}

func TestStatementsGoParseFailureIsErr(t *testing.T) {
	if _, _, err := statements(memfile("bad.go", "package db\n\nfunc broken( {\n")); err == nil {
		t.Fatal("a .go AST parse failure must be err (exit 2)")
	}
}

func TestStatementsNonTargetFileIsNil(t *testing.T) {
	stmts, fails, err := statements(memfile("README.md", "SELECT 1;"))
	if err != nil || stmts != nil || fails != nil {
		t.Fatalf("non .sql/.go must be all-nil: %v %+v %+v", err, stmts, fails)
	}
}

func TestStatementsSQLLeadingCommentLine(t *testing.T) {
	stmts, _, err := statements(memfile("q.sql", "-- migration: widgets\nSELECT 1;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 || stmts[0].Line != 2 {
		t.Fatalf("want first stmt on line 2 (after the leading comment), got %+v", stmts)
	}
}

func TestStatementsSQLCommentBetweenStatements(t *testing.T) {
	stmts, _, err := statements(memfile("q.sql", "SELECT 1;\n-- next\nSELECT 2;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 || stmts[1].Line != 3 {
		t.Fatalf("want 2nd stmt on line 3, got %+v", stmts)
	}
}

func TestStatementsSQLBlockCommentLine(t *testing.T) {
	stmts, _, err := statements(memfile("q.sql", "/* header\n   spanning */\nSELECT 1;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 || stmts[0].Line != 3 {
		t.Fatalf("want first stmt on line 3 (after the block comment), got %+v", stmts)
	}
}

func TestStatementsSQLBlankLineSeparation(t *testing.T) {
	stmts, _, err := statements(memfile("q.sql", "SELECT 1;\n\nSELECT 2;\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 || stmts[1].Line != 3 {
		t.Fatalf("want 2nd stmt on line 3, got %+v", stmts)
	}
}

func TestStatementsSQLErrorLineFromCursor(t *testing.T) {
	// syntax error on line 3 must be reported at line 3, not line 1.
	stmts, fails, err := statements(memfile("m.sql", "SELECT 1;\nSELECT 2;\nSELECT FROM;\n"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(stmts) != 0 || len(fails) != 1 {
		t.Fatalf("want 0 stmts / 1 failure, got %d / %d", len(stmts), len(fails))
	}
	if fails[0].Line != 3 {
		t.Fatalf("want failure on line 3 (cursor), got %d", fails[0].Line)
	}
}

// Fix #11: lockingStatements' cheap pre-parse gate. Every locking clause (FOR
// UPDATE, FOR NO KEY UPDATE, FOR SHARE, FOR KEY SHARE) contains "update" or
// "share", so a file whose lowercased content contains neither can be skipped
// without parsing — conservatively, since it can never hold a real lock.

func TestLockingStatementsSkipsSQLFileWithNoUpdateOrShare(t *testing.T) {
	stmts, err := lockingStatements(memfile("t.sql", "CREATE TABLE t (id int);\n"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stmts != nil {
		t.Fatalf("want nil stmts for a file with no update/share word, got %+v", stmts)
	}
}

func TestLockingStatementsSkipsGoFileWithNoUpdateOrShare(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT * FROM t\" }\n"
	stmts, err := lockingStatements(memfile("db.go", src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stmts != nil {
		t.Fatalf("want nil stmts for a .go file with no update/share word, got %+v", stmts)
	}
}

func TestLockingStatementsStillParsesFileWithLockClause(t *testing.T) {
	// The gate must never produce a false negative: a real FOR UPDATE clause
	// (containing "update") must still be parsed and returned.
	stmts, err := lockingStatements(memfile("q.sql", "SELECT * FROM t WHERE id = $1 FOR UPDATE;\n"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("want 1 stmt for a file containing FOR UPDATE, got %d", len(stmts))
	}
}

func TestLockingStatementsStillParsesFileWithShareClause(t *testing.T) {
	// "share" alone (FOR SHARE / FOR KEY SHARE) must also still be parsed.
	stmts, err := lockingStatements(memfile("q.sql", "SELECT * FROM t WHERE id = $1 FOR SHARE;\n"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("want 1 stmt for a file containing FOR SHARE, got %d", len(stmts))
	}
}

func TestLockingStatementsMalformedGoNoLockKeywordStillErrors(t *testing.T) {
	// [fail-closed] a malformed .go file must exit-2 via the locking rule even
	// when it contains no lock keyword — the Go-AST parse error must not be
	// swallowed by the lock-keyword skip.
	_, err := lockingStatements(memfile("bad.go", "package db\n\nfunc broken( {\n"))
	if err == nil {
		t.Fatal("a malformed .go file must return an error (exit 2), not a silent pass")
	}
}

func TestLockingStatementsSplitKeywordNotSkipped(t *testing.T) {
	// "FOR UPD" + "ATE" reassembles to FOR UPDATE — must NOT be skipped.
	src := "package db\n\nfunc q() string { return \"SELECT * FROM t WHERE status='x' FOR UPD\" + \"ATE\" }\n"
	stmts, err := lockingStatements(memfile("q.go", src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("split lock keyword must reassemble and be parsed: got %d stmts", len(stmts))
	}
}

// Fix: pg_query reports its error cursor as a CHARACTER position, but lineAt
// counts newlines in a BYTE-sliced prefix. With multibyte text before the
// error the two diverge and the finding lands on the wrong line.

func TestStatementsSQLErrorLineFromCursorWithMultibytePrefix(t *testing.T) {
	// Two comment lines of em dashes (3 bytes each, 1 char each) precede the
	// syntax error on line 3; a byte-indexed cursor stops inside line 1.
	dashes := strings.Repeat("—", 30)
	src := "-- " + dashes + "\n-- " + dashes + "\nSELECT FROM;\n"
	stmts, fails, err := statements(memfile("m.sql", src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(stmts) != 0 || len(fails) != 1 {
		t.Fatalf("want 0 stmts / 1 failure, got %d / %d", len(stmts), len(fails))
	}
	if fails[0].Line != 3 {
		t.Fatalf("want failure on line 3 despite the multibyte prefix, got %d", fails[0].Line)
	}
}

func TestStatementsSQLStatementLineWithMultibytePrefix(t *testing.T) {
	// Companion to the cursor test: RawStmt.StmtLocation is a BYTE offset, so
	// the success path needs no conversion. Pin that, so a future change to
	// one offset path does not get mirrored onto the other.
	dashes := strings.Repeat("—", 30)
	src := "-- " + dashes + "\n-- " + dashes + "\nSELECT 1;\nSELECT 2;\n"
	stmts, fails, err := statements(memfile("m.sql", src))
	if err != nil || len(fails) != 0 {
		t.Fatalf("clean sql: err=%v fails=%+v", err, fails)
	}
	if len(stmts) != 2 {
		t.Fatalf("want 2 stmts, got %d", len(stmts))
	}
	if stmts[0].Line != 3 || stmts[1].Line != 4 {
		t.Fatalf("want stmts on lines 3 and 4, got %d and %d", stmts[0].Line, stmts[1].Line)
	}
}

func TestSkipInsignificantNegativeOffsetNoPanic(t *testing.T) {
	if got := skipInsignificant("SELECT 1", -5); got < 0 {
		t.Fatalf("negative offset must be clamped, got %d", got)
	}
}
