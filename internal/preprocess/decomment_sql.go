package preprocess

// DecommentSQL blanks SQL comments and keeps everything else — the complement
// of CommentsOnlySQL. `--` line comments and `/* … */` block comments (which
// NEST in PostgreSQL) become spaces, delimiters included; every other byte
// survives verbatim. Newlines are preserved, so the result has the same length
// and the same line structure as the input: byte offset N in is byte offset N
// out, and a consumer that counts newlines to report a line number reads the
// same number before and after.
//
// It shares CommentsOnlySQL's lexer (sqlScanComments), so the rules for what is
// NOT a comment are stated once rather than twice: `'…'` with doubled-quote and
// E-string `\` escapes, `"…"` quoted identifiers with `""` escapes, and
// `$tag$…$tag$` dollar-quoted bodies are string data, and a `--` inside one is
// row content rather than a comment. `$1` does not open a dollar-quote. An
// unterminated comment runs to end of input; an unterminated string swallows
// the rest, so no comment is found inside it. Those are the existing lexer's
// semantics — this function inherits them, it does not restate them.
//
// DELIBERATELY NOT REGISTERED as a `preprocess:` vocabulary word. A registered
// transform is applied to whole source files as the engine scans them, and the
// caller that needs this one — the sql/statement-predicate rule type — works on
// SQL that sqlextract has already lifted OUT of Go string literals. Running SQL
// lexing over raw Go source fails in the silent direction: the SQL sits inside
// `"…"`, which this lexer reads as a quoted identifier, so a comment inside it
// is never seen and the transform would report success having done nothing.
// Registering it is a separate decision with its own vocabulary cost (Names(),
// the AGENTS.md map, `formwork list preprocessors`); it is not owed by #137.
func DecommentSQL(src []byte) []byte {
	out := append([]byte(nil), src...)
	sqlScanComments(src, func(start, end int) { blank(out, start, end) }, nil)
	return out
}
