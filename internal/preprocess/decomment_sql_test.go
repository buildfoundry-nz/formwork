package preprocess

import (
	"strings"
	"testing"
)

// DecommentSQL is the projection a rule needs when it asks "does the CODE say
// this?" — the inverse of the comments-only family's "is this token inside a
// comment?". sql/statement-predicate matches token regexes over statement text,
// so without this projection a comment carrying a required token satisfies the
// requirement while the code omits it (#137).
//
// The contract these tests state: comment text and its delimiters become
// spaces, everything else survives verbatim, and line structure is preserved so
// a consumer counting newlines reports the same line before and after.

// decomment applies DecommentSQL and enforces the line-preserving contract on
// every call. It resolves the function directly rather than through Lookup:
// DecommentSQL is deliberately not a registered `preprocess:` word (see its
// doc), so the registry would never find it — but the length and newline
// invariants project() enforces for the registered family still apply, because
// sqltext maps offsets in the result back to source line numbers.
func decomment(t *testing.T, src string) string {
	t.Helper()
	out := string(DecommentSQL([]byte(src)))
	if got, want := strings.Count(out, "\n"), strings.Count(src, "\n"); got != want {
		t.Fatalf("line structure changed: %d newlines, want %d\n--- got ---\n%s", got, want, out)
	}
	if len(out) != len(src) {
		t.Fatalf("length changed: %d bytes, want %d\n--- got ---\n%s", len(out), len(src), out)
	}
	return out
}

func assertDecomment(t *testing.T, src string, keep, gone []string) {
	t.Helper()
	out := decomment(t, src)
	for _, s := range keep {
		if !strings.Contains(out, s) {
			t.Errorf("code text %q was dropped\n--- got ---\n%s", s, out)
		}
	}
	for _, s := range gone {
		if strings.Contains(out, s) {
			t.Errorf("comment text %q survived\n--- got ---\n%s", s, out)
		}
	}
}

func TestDecommentSQLBlanksLineAndBlockComments(t *testing.T) {
	src := "-- line comment body\n" +
		"INSERT INTO t (a, b) -- trailing comment body\n" +
		"/* block comment first line\n" +
		"   block comment continuation line\n" +
		"*/\n" +
		"VALUES (1, 2);\n"
	assertDecomment(t, src,
		[]string{"INSERT INTO t (a, b)", "VALUES (1, 2);"},
		[]string{"line comment body", "trailing comment body",
			"block comment first line", "block comment continuation line"})
}

// The string rules are the whole reason this cannot be a regex. A `--` inside a
// literal is row data; treating it as a comment opener blanks live code to the
// end of the line, which is the missed-violation direction.
func TestDecommentSQLKeepsCommentShapedBytesInsideStrings(t *testing.T) {
	src := "INSERT INTO t (note, line_class) VALUES ('-- not a comment', 'x');\n" +
		"INSERT INTO t (note, line_class) VALUES (E'-- \\' still not a comment', 'y');\n" +
		"INSERT INTO t (note, line_class) VALUES ($fn$-- nor this$fn$, 'z');\n" +
		"SELECT \"-- quoted identifier\" FROM t;\n"
	assertDecomment(t, src,
		[]string{"line_class", "-- not a comment", "still not a comment",
			"nor this", "-- quoted identifier"},
		nil)
}

// Nesting is the difference between "the comment ends at the first */" and what
// PostgreSQL actually does. A naive scanner treats `col_b */` as live code.
func TestDecommentSQLNestsBlockComments(t *testing.T) {
	src := "INSERT INTO t (col_a) /* outer /* inner */ col_b */ VALUES (1);\n"
	assertDecomment(t, src,
		[]string{"INSERT INTO t (col_a)", "VALUES (1);"},
		[]string{"outer", "inner", "col_b"})
}

// `$1` is a positional parameter, not a dollar-quote opener. If it opened one,
// the rest of the statement would read as string data and the trailing comment
// would survive — the same missed-violation direction as the string case.
func TestDecommentSQLPositionalParameterIsNotADollarQuote(t *testing.T) {
	src := "INSERT INTO t (col_a) VALUES ($1); -- col_b is set by a follow-up UPDATE\n"
	assertDecomment(t, src,
		[]string{"INSERT INTO t (col_a) VALUES ($1);"},
		[]string{"col_b is set by a follow-up UPDATE"})
}

// An unterminated `/*` is a comment to end of input — the existing lexer's
// reading, and the fail-closed one for a `require`: the tail cannot satisfy it.
func TestDecommentSQLUnterminatedBlockRunsToEndOfInput(t *testing.T) {
	src := "INSERT INTO t (col_a) /* col_b never closed\nVALUES (1);\n"
	assertDecomment(t, src,
		[]string{"INSERT INTO t (col_a)"},
		[]string{"col_b never closed", "VALUES (1);"})
}

// The Transform contract in this package's doc: never write to the argument.
// scan hands every rule the SAME cached slice, and scan.File.Lines aliases it
// (#66), so an in-place edit corrupts what every other rule in the run sees.
func TestDecommentSQLDoesNotMutateItsArgument(t *testing.T) {
	src := []byte("INSERT INTO t (a) VALUES (1); -- comment\n")
	before := string(src)
	DecommentSQL(src)
	if string(src) != before {
		t.Fatalf("DecommentSQL wrote to its argument:\n got %q\nwant %q", src, before)
	}
}

// Blanking must not collapse a multi-line comment onto one line: sqltext counts
// newlines to report a finding's line, so a lost newline shifts every finding
// after it.
func TestDecommentSQLPreservesNewlinesInsideBlockComments(t *testing.T) {
	src := "/* a\n b\n c */\nDELETE FROM users;\n"
	out := decomment(t, src)
	if idx := strings.Index(out, "DELETE"); strings.Count(out[:idx], "\n") != 3 {
		t.Fatalf("DELETE must still start on line 4, got %d newlines before it\n--- got ---\n%s",
			strings.Count(out[:idx], "\n"), out)
	}
}
