package preprocess

func init() { Register("code-only-dart", CodeOnlyDart) }

// CodeOnlyDart keeps only the CODE of a Dart source file: comment bytes
// (`//`, `///`, and the nesting `/* … */` form) and the CONTENTS of every
// string literal become spaces. The quote delimiters themselves survive, so
// the projection still reads as Dart — an empty string where a literal was —
// and a rule can tell "a string was here" from "nothing was here".
//
// It is the complement of [CommentsOnlyDart] and shares that function's lexer
// shape, including its two bounding decisions: block comments NEST, and a
// single-/double-quoted string is closed at a newline as well as at its quote
// so an unbalanced quote costs one line of projection rather than the rest of
// the file (#9272).
//
// Blanking string contents is what makes DELIMITER COUNTING sound. Dart string
// interpolation puts arbitrary code — including parentheses and braces — inside
// a literal (`'${label(x)}'`), and a rule that walks an argument list by
// bracket depth over raw source would take those as real brackets and lose the
// end of the invocation. Under this projection an interpolation contributes no
// delimiters at all.
func CodeOnlyDart(src []byte) []byte {
	out := append([]byte(nil), src...)
	n := len(src)
	state, depth, raw := dartCode, 0, false
	for i := 0; i < n; {
		switch state {
		case dartCode:
			switch {
			case has(src, i, "//"):
				j := i + 2
				for j < n && src[j] != '\n' {
					j++
				}
				blank(out, i, j)
				i = j
			case has(src, i, "/*"):
				state, depth = dartBlock, 1
				blank(out, i, i+2)
				i += 2
			case src[i] == 'r' && i+1 < n && (src[i+1] == '\'' || src[i+1] == '"'):
				// Raw-string prefix: the `r` and the quote(s) are delimiters
				// and stay; only the contents are blanked.
				q := src[i+1]
				if has(src, i+1, string([]byte{q, q, q})) {
					state, raw = tripleState(q), true
					i += 4
					continue
				}
				state, raw = singleState(q), true
				i += 2
			case has(src, i, "'''"):
				state, raw = dartS3, false
				i += 3
			case has(src, i, `"""`):
				state, raw = dartD3, false
				i += 3
			case src[i] == '\'':
				state, raw = dartS1, false
				i++
			case src[i] == '"':
				state, raw = dartD1, false
				i++
			default:
				i++
			}
		case dartBlock:
			switch {
			case has(src, i, "/*"):
				depth++
				blank(out, i, i+2)
				i += 2
			case has(src, i, "*/"):
				depth--
				blank(out, i, i+2)
				i += 2
				if depth == 0 {
					state = dartCode
				}
			default:
				blank(out, i, i+1)
				i++
			}
		case dartS1, dartD1:
			q := byte('\'')
			if state == dartD1 {
				q = '"'
			}
			switch {
			case !raw && src[i] == '\\':
				blank(out, i, min(i+2, n))
				i += 2
			case src[i] == q || src[i] == '\n':
				// The closing quote survives; a newline closes the literal
				// without being consumed as content.
				state, raw = dartCode, false
				i++
			default:
				blank(out, i, i+1)
				i++
			}
		case dartS3, dartD3:
			q := byte('\'')
			if state == dartD3 {
				q = '"'
			}
			switch {
			case !raw && src[i] == '\\':
				blank(out, i, min(i+2, n))
				i += 2
			case has(src, i, string([]byte{q, q, q})):
				state, raw = dartCode, false
				i += 3
			default:
				blank(out, i, i+1)
				i++
			}
		}
	}
	return out
}
