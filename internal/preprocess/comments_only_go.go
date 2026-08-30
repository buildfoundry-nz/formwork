package preprocess

func init() { Register("comments-only-go", CommentsOnlyGo) }

// CommentsOnlyGo keeps only the CONTENTS of Go comments: the text of `//` line
// comments and `/* … */` block comments survives verbatim; code, string/rune/raw
// literal contents, and every delimiter (including the `//`, `/*` and `*/`
// themselves) become spaces. It is the third projection of the shared Go lexer
// family — DecommentGo keeps code, StringsOnlyGo keeps string contents, this
// keeps comment contents.
//
// It answers the question an annotation-marker rule needs: "is this token inside
// a COMMENT — regardless of where in the comment it sits?" A comment-opener
// regex cannot answer that. It (a) misses a marker on a bare continuation line
// of a multi-line block comment, because that line carries no comment token at
// all, and (b) accuses comment-shaped bytes INSIDE a string literal, where an
// annotation spelled inside a `"// … "` prefix is data — which is exactly the
// shape of the marker rules' own pattern tables. Because the delimiters are
// blanked, a consuming rule
// needs no comment-opener anchor in its pattern at all — being present in this
// projection IS being in a comment.
//
// Contributed from the validating port, where it replaced a
// hand-maintained awk lexer of the same contract. That awk remains live there
// on a separate path, so downstream holds the two to one shared case table;
// upstream's copy of that table is comments_only_test.go in this package.
func CommentsOnlyGo(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank(out, 0, len(out))
	for _, r := range scanGo(src) {
		switch r.kind {
		case kindLineComment:
			// scanGo excludes the newline, so r.end is the line end.
			keep(out, src, r.start+len("//"), r.end)
		case kindBlockComment:
			start, end := r.start+len("/*"), r.end
			// Drop the closing delimiter if the comment was terminated;
			// an unterminated /* runs to EOF and has none.
			if end-2 >= start && src[end-2] == '*' && src[end-1] == '/' {
				end -= 2
			}
			keep(out, src, start, end)
		}
	}
	return out
}

// keep copies src[start:end) back into a blanked out, restoring that span
// verbatim. Out-of-range or inverted spans copy nothing.
func keep(out, src []byte, start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(src) {
		end = len(src)
	}
	if start >= end {
		return
	}
	copy(out[start:end], src[start:end])
}
