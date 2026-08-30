package sqltext_test

import (
	"strings"
	"testing"
)

// A ';' inside a SQL string literal is DATA, not a statement terminator (#139).
//
// splitStatements used to scan for ';' one byte at a time with no lexical
// state, so a literal carrying a semicolon truncated the statement at it. That
// is the opposite direction from #137: this one FAILS CONFORMING CODE. The
// tail after the literal became its own unjudged "statement" as well, so the
// same text could be dropped or (where the table name survived into it) judged
// twice.
//
// The control tests below matter as much as the firing ones: a ';' in CODE
// must still terminate a statement, and each statement must still report the
// line it starts on.

// The reported shape, verbatim from the issue (table name genericized): the
// required token is named in the code AFTER a literal containing a ';'.
func TestSemicolonInSingleQuotedLiteralDoesNotSplitStatement(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.t (note) SELECT 'a;b' FROM src WHERE line_class IS NOT NULL;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("a ';' inside a string literal must not terminate the statement: %+v", ms)
	}
}

// Two single quotes in a row are an escaped quote, so a ';' after them is
// still data.
//
// What guards this test is the literal skip itself, NOT sqlQuotedEnd's
// doubled-quote branch: deleting that branch leaves the whole package green,
// because mis-pairing regroups the quotes into the same inside-span (the two
// quotes of an escaped pair are adjacent, so the gap it opens between them is
// empty). The branch is kept for parity with PostgreSQL's lexer, not because
// this seam can observe it — said here so a later reader does not take this
// test for coverage of it.
func TestSemicolonAfterDoubledQuoteInsideLiteralDoesNotSplitStatement(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.t (note) SELECT 'it''s a;b' FROM src WHERE line_class IS NOT NULL;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("'' is an escaped quote, so the ';' after it is still data: %+v", ms)
	}
}

// An escape string E'…' takes backslash escapes, so E'a\';b' holds the ';'
// too — the escaped quote does not close the literal.
func TestSemicolonInEscapeStringDoesNotSplitStatement(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.t (note) SELECT E'a\\';b' FROM src WHERE line_class IS NOT NULL;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("a backslash-escaped quote in E'…' does not close the literal: %+v", ms)
	}
}

// A quoted identifier ("…") is not a comment and not a terminator either.
func TestSemicolonInQuotedIdentifierDoesNotSplitStatement(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.t (\"odd;name\") SELECT 1 FROM src WHERE line_class IS NOT NULL;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("a ';' inside a quoted identifier must not terminate the statement: %+v", ms)
	}
}

// A dollar-quoted body is a string literal with no escapes at all — the whole
// function body, semicolons included, is one statement's data.
func TestSemicolonInDollarQuotedBodyDoesNotSplitStatement(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	// The table name sits before the body's interior ';' and the required token
	// after it, so a split at that ';' selects a truncated statement that is
	// missing the token — while the real single statement carries both.
	body := "CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1 FROM palletra.t; SELECT line_class $body$ LANGUAGE plpgsql;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("a ';' inside $tag$…$tag$ must not terminate the statement: %+v", ms)
	}
}

// A '$' glued onto an identifier character continues that identifier and opens
// nothing — so `col$a$name` must not swallow the rest of the file as a
// dollar-quoted body. Without the adjacency guard the second statement below
// disappears and its violation is never reported.
func TestGluedDollarDoesNotOpenAQuotedBody(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "SELECT col$a$name FROM src;\n" +
		"INSERT INTO palletra.t (note) VALUES ('x');\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("a glued '$' opens no literal, so the second statement must still be judged: %+v", ms)
	}
	if ms[0].Line != 2 {
		t.Fatalf("want the violation on line 2, got %d", ms[0].Line)
	}
}

// --- controls: a ';' in CODE still terminates, with the right line ---

// Two statements separated by a real terminator: only the second violates, and
// it reports its own line. This is the property the fix must not trade away —
// skipping literals must not skip the newlines inside them either, or every
// line number after a multi-line literal drifts.
func TestSemicolonInCodeStillSplitsAndKeepsLines(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.t (note) VALUES ('multi\nline;literal') RETURNING line_class;\n" +
		"INSERT INTO palletra.t (note) VALUES ('x');\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("the second statement violates and must be reported once: %+v", ms)
	}
	// The literal on the first statement spans a newline; the second statement
	// begins on line 3 of the file.
	if ms[0].Line != 3 {
		t.Fatalf("want the violation on line 3 (newlines inside the literal still counted), got %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "line_class") {
		t.Fatalf("message should name the missing token: %s", ms[0].Message)
	}
}

// --- unbalanced text: the literal skip must never HIDE a statement ---

// A ';' the lexer cannot classify is still a terminator (#139 review).
//
// Skipping literals is only sound while the scan knows which bytes are data.
// An UNTERMINATED literal is the case where it does not: every byte after the
// opening quote is unclassifiable, and reading them all as data merges the rest
// of the text into the statement that opened the quote — so a `require`
// satisfied before the quote covers every governed statement after it, and they
// are never reported. That is this engine's signature defect, a pass the check
// did not earn, and it is worse than the false positive #139 removed.
//
// So the split falls back to the pre-#139 byte-wise scan for any text that does
// not lex. Over-splitting can only ever ADD statements to judge; merging them
// removes statements from judgement.
//
// Mutation: make sqlQuotedEnd return len(s) rather than -1 for an unterminated
// literal (the swallowing direction) — both tests below go red.
func TestUnterminatedLiteralDoesNotHideALaterStatement(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	// The require is satisfied BEFORE the stray quote; the governed DELETE
	// after it is not covered by it.
	body := "SELECT line_class FROM src WHERE note = 'x; DELETE FROM palletra.t\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("the DELETE after an unterminated literal must still be judged: %+v", ms)
	}
}

// The same defect at file scale: one stray apostrophe near the top of a .sql
// file disabled the rule for every statement below it.
func TestUnterminatedLiteralDoesNotDisableTheRestOfTheFile(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	body := "SELECT line_class FROM src WHERE a = 'x;\n" +
		"INSERT INTO palletra.t (note) VALUES ('y');\n" +
		"INSERT INTO palletra.t (note) VALUES ('z');\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 2 {
		t.Fatalf("both governed INSERTs must be judged, not swallowed: %+v", ms)
	}
}

// A FRAGMENT of a composition sqlextract could not reassemble may BEGIN inside
// a string literal, and then every quote in it pairs one out of phase: what the
// scan reads as a literal is code, and the terminators inside it disappear.
//
// The middle fragment below holds an EVEN number of quotes, so it lexes
// "cleanly" — nothing is left unterminated and the unterminated-literal
// fallback never fires. The phase is simply wrong from its first byte, and no
// amount of looking at the fragment alone can tell. What does tell is
// sqlextract's Candidate.Partial: this text is a piece of a composition it
// could not reassemble, so the literal skip is not applied to it at all.
//
// Mutation: pass `true` instead of `!cand.Partial` in goStatements — the
// governed UPDATE is swallowed into a statement whose require the SELECT
// satisfies, one match instead of two, and this test goes red.
func TestPartialFragmentDoesNotPairQuotesOutOfPhase(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+
		`require:`+"\n"+
		`  - '\bline_class\b'`+"\n")
	src := "package p\n\nfunc f(v, w string) string {\n" +
		"\treturn \"INSERT INTO palletra.t (note) VALUES ('\" + v +\n" +
		"\t\t\"'); SELECT line_class FROM src; UPDATE palletra.t SET note = '\" + w + \"'\"\n}\n"
	ms := check(t, c, file("q.go", src))
	if len(ms) != 2 {
		t.Fatalf("want the truncated INSERT and the governed UPDATE both judged, got %d: %+v", len(ms), ms)
	}
	if !strings.Contains(ms[1].Message, "UPDATE palletra.t") {
		t.Fatalf("the second match should be the UPDATE, got %s", ms[1].Message)
	}
}
