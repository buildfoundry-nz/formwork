package sqltext_test

import "testing"

// SQL comments are not statement text (#137).
//
// `require` and `forbid` are token regexes matched over the whole statement, so
// until the engine strips comments a token that appears ONLY in a comment
// satisfies a requirement the code does not meet. That is not an adversarial
// shape: a developer writing a comment to explain why a column is absent is
// exactly the author the rule needs to stop, and the explanation turns it
// green. Reproduced downstream in the validating port.
//
// A per-rule regex cannot close it. Anchoring to the column list
// (`[^(;]*\([^)]*` — reach the opening paren without crossing another, then find
// the token before the list closes) rules out a comment placed BEFORE the list,
// which is what the validating port shipped as a patch. A comment placed
// INSIDE the list still satisfies it, and RE2 has no negative lookahead, so no
// refinement of the pattern expresses "not in a comment". The engine is the
// only altitude.
//
// The control tests below matter as much as the firing ones: the fix must not
// blank comment-SHAPED bytes that live inside string literals, because that
// deletes live code from the statement — the missed-violation direction.

// --- the evasion, in the shapes it actually takes ---

// The reported shape, verbatim from the validating port's report: the required column
// is named only in a comment in the gap between the table and the column list.
func TestCommentBeforeColumnListDoesNotSatisfyRequire(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.bom_line_items'`+"\n"+
		`require:`+"\n"+
		`  - 'INSERT\s+INTO\s+palletra\.bom_line_items[^)]*\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.bom_line_items -- line_class is set by a follow-up UPDATE\n" +
		"  (project_id, bom_id, ordinal)\n" +
		"VALUES (1, 2, 3);\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("a comment naming the column must not satisfy the requirement: %+v", ms)
	}
}

// The half no anchor reaches: the comment sits INSIDE the column list, so even
// the tightened `[^(;]*\([^)]*` anchor finds the token where it wants one.
func TestCommentInsideColumnListDoesNotSatisfyRequire(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.bom_line_items'`+"\n"+
		`require:`+"\n"+
		`  - 'INSERT\s+INTO\s+palletra\.bom_line_items[^(;]*\([^)]*\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.bom_line_items\n" +
		"  (project_id, bom_id /* line_class deliberately omitted */)\n" +
		"VALUES (1, 2);\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("a comment inside the column list must not satisfy the requirement: %+v", ms)
	}
}

// The rules that carry this requirement are scoped to .go files — the SQL is
// reassembled out of Go literals by sqlextract before any predicate runs, so the
// strip has to happen after reassembly, not on the raw file.
func TestCommentInGoLiteralDoesNotSatisfyRequire(t *testing.T) {
	src := "package db\n" +
		"\n" +
		"const insertLine = `INSERT INTO palletra.bom_line_items -- line_class set later\n" +
		"\t(project_id, bom_id)\n" +
		"VALUES ($1, $2)`\n"
	c := mustChecker(t, `table: 'palletra\.bom_line_items'`+"\n"+
		`require:`+"\n"+
		`  - 'INSERT\s+INTO\s+palletra\.bom_line_items[^)]*\bline_class\b'`+"\n")
	ms := check(t, c, file("db.go", src))
	if len(ms) != 1 {
		t.Fatalf("a comment inside a Go SQL literal must not satisfy the requirement: %+v", ms)
	}
}

// The other direction: a forbidden token mentioned in a comment is a FALSE
// POSITIVE today — the rule accuses code that does not do the thing.
func TestForbiddenTokenInCommentDoesNotFire(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nforbid:\n  - 'SELECT \\*'\n")
	body := "DELETE FROM users -- never SELECT * from users here\n" +
		"  WHERE id = 1;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("a forbidden token mentioned only in a comment must not fire: %+v", ms)
	}
}

// Comment text must not SELECT a statement either: a rule scoped to one table
// otherwise judges a statement that merely mentions it in prose.
func TestTableSelectorIgnoresCommentText(t *testing.T) {
	c := mustChecker(t, "table: 'orders'\nrequire:\n  - 'WHERE'\n")
	ms := check(t, c, file("q.sql", "DELETE FROM users -- also affects orders\n"))
	if len(ms) != 0 {
		t.Fatalf("a table named only in a comment must not select the statement: %+v", ms)
	}
}

// splitStatements breaks on ';'. A semicolon inside a comment is not a statement
// terminator, and splitting there truncates the statement — the tail carrying
// the required token becomes a separate segment that the table regex no longer
// selects.
func TestSemicolonInCommentDoesNotSplitStatement(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	body := "DELETE FROM users -- careful; this one is scoped\n" +
		"  WHERE id = 1;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 0 {
		t.Fatalf("a ';' inside a comment must not split the statement: %+v", ms)
	}
}

// --- line numbers survive the strip ---

// Blanking must be length- and newline-preserving: every finding's reported line
// is derived by counting newlines, so a comment collapsed to one line would
// shift every finding after it.
func TestCommentStrippingPreservesFindingLine(t *testing.T) {
	c := mustChecker(t, "table: 'users'\nrequire:\n  - 'WHERE'\n")
	body := "/* a\n" +
		"   b\n" +
		"   c */\n" +
		"DELETE FROM users;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("the DELETE without WHERE must violate: %+v", ms)
	}
	if ms[0].Line != 4 {
		t.Fatalf("want the finding on line 4 (after the 3-line block comment), got %d", ms[0].Line)
	}
}

// --- controls: comment-shaped bytes that are NOT comments ---

// A `--` inside a string literal is row data. Reading it as a comment opener
// blanks the rest of the line, deleting live code from the statement — a rule
// failing conforming code.
//
// The required token sits AFTER the string on purpose. Put it before and the
// test passes whether or not the string is lexed correctly, which is how a
// control quietly stops controlling anything.
func TestCommentMarkerInsideStringLiteralIsNotAComment(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+`require:`+"\n"+`  - '\bline_class\b'`+"\n")
	ms := check(t, c, file("q.sql",
		"INSERT INTO palletra.t (note) SELECT '-- not a comment' FROM src WHERE line_class IS NOT NULL;\n"))
	if len(ms) != 0 {
		t.Fatalf("a '--' inside a string literal must not blank the rest of the line: %+v", ms)
	}
}

// Same trap through a dollar-quoted body, where there are no escapes at all.
func TestCommentMarkerInsideDollarQuotedBodyIsNotAComment(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+`require:`+"\n"+`  - '\bline_class\b'`+"\n")
	ms := check(t, c, file("q.sql",
		"INSERT INTO palletra.t (note) SELECT $fn$-- not a comment$fn$ FROM src WHERE line_class IS NOT NULL;\n"))
	if len(ms) != 0 {
		t.Fatalf("a '--' inside a dollar-quoted body must not blank the rest of the line: %+v", ms)
	}
}

// `$1` is a positional parameter. If it opened a dollar-quote, everything after
// it would read as string data, the comment would survive unstripped, and the
// requirement would be satisfied by the comment — so this test discriminates in
// both directions. The comment is inside the statement, before the `;`, because
// a comment in a segment of its own is dropped for a different reason.
func TestPositionalParameterDoesNotOpenADollarQuote(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+`require:`+"\n"+`  - '\bline_class\b'`+"\n")
	body := "INSERT INTO palletra.t (project_id) SELECT $1 -- line_class is set by a follow-up UPDATE\n" +
		"  FROM src;\n"
	ms := check(t, c, file("q.sql", body))
	if len(ms) != 1 {
		t.Fatalf("'$1' must not open a dollar-quote that hides the trailing comment: %+v", ms)
	}
}

// PostgreSQL block comments NEST. A scanner that stops at the first `*/` treats
// `line_class */` as live code and the requirement passes.
func TestNestedBlockCommentIsFullyStripped(t *testing.T) {
	c := mustChecker(t, `table: 'palletra\.t\b'`+"\n"+`require:`+"\n"+`  - '\bline_class\b'`+"\n")
	ms := check(t, c, file("q.sql",
		"INSERT INTO palletra.t (project_id) /* outer /* inner */ line_class */ VALUES (1);\n"))
	if len(ms) != 1 {
		t.Fatalf("a nested block comment must be stripped whole: %+v", ms)
	}
}
