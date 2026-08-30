package preprocess

func init() { Register("comments-only-sql", CommentsOnlySQL) }

// CommentsOnlySQL keeps only the CONTENTS of SQL comments: the text of `--` line
// comments and `/* … */` block comments — which NEST in PostgreSQL — survives
// verbatim; code, string contents (`'…'` with doubled-quote and E-string `\`
// escapes, `"…"` quoted identifiers with `""` escapes, and `$tag$…$tag$`
// dollar-quoted bodies), and every delimiter become spaces. Same contract and
// same purpose as CommentsOnlyGo; see that doc comment for why a comment-opener
// regex cannot substitute — a dollar-quoted function body is a string literal,
// so a `--` inside one is row data, not a comment.
//
// The lexical rules live in sqlScanComments (sql_lex.go), shared with
// DecommentSQL, which is this function's complement. This is the sole
// definition of the keep-comments contract; tested at comments_only_test.go in
// this package.
//
// Contributed from the validating port, where it replaced a
// hand-maintained awk lexer that was deleted once this became the only
// implementation.
func CommentsOnlySQL(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank(out, 0, len(out))
	sqlScanComments(src, nil, func(start, end int) { keep(out, src, start, end) })
	return out
}
