package preprocess

func init() { Register("decomment-sh", DecommentSh) }

// DecommentSh is the fourth projection of the one shell lexer (scanSh): it
// blanks ONLY the blankable-comment spans and leaves everything else —
// string interiors, heredoc bodies, code — matchable. It exists for gates
// ported from bash pipelines whose comment handling was `grep -v
// '^[[:space:]]*#'` (strip comment lines, keep string contents): the bash
// buf-breaking baseline gate is the case in point, where the forbidden shape
// `branch=staging` lives INSIDE a quoted --against ref, so DestringSh's
// string blanking would silence the gate and DestringDecommentSh's pair of
// blankings doubly so.
//
// The blanked-comment boundary is the same narrow one DestringDecommentSh
// documents (isCommentBlankBoundary): a '#' blanks to end-of-line only at
// start-of-line or after a space/tab, matching the reference stripper
// `sed -E 's/(^|[[:space:]])#.*$//'`. A `cmd;# ...` comment is skipped by the
// lexer but NOT blanked; ${x#foo}, $#, and url#fragment are never comment
// starts. Because string interiors survive, an apostrophe inside a blanked
// comment can never open a phantom string either — the lexer still skips the
// comment before quote state is considered.
func DecommentSh(src []byte) []byte {
	out := append([]byte(nil), src...)
	_, comments := scanSh(src)
	for _, s := range comments {
		blank(out, s.start, s.end)
	}
	return out
}
