package preprocess

func init() { Register("comments-only-dart", CommentsOnlyDart) }

// dartState is the lexer state of CommentsOnlyDart.
type dartState int

const (
	dartCode  dartState = iota
	dartBlock           // /* … */, NESTS in Dart
	dartS1              // '…'
	dartD1              // "…"
	dartS3              // '''…''', may span lines
	dartD3              // """…""", may span lines
)

// CommentsOnlyDart keeps only the CONTENTS of Dart comments: the text of
// `//`/`///` line comments and `/* … */` block comments — which NEST in Dart —
// survives verbatim; code, string contents (single `'`, double `"`, the two
// triple-quoted forms, and the raw `r'…'`/`r"…"` forms), and every delimiter
// become spaces. Same contract and same purpose as CommentsOnlyGo; see that doc
// comment for why a comment-opener regex cannot substitute.
//
// Contributed from the validating port, where it replaced a
// hand-maintained awk lexer that was deleted once this became the only
// implementation. This is the sole definition of the contract; tested at
// comments_only_test.go in this package.
//
// One deliberate difference from the awk it replaced: a single-quote/double-quote string is
// closed at a newline as well as at its quote. Dart's non-triple string forms
// cannot span lines, and bounding them means an unbalanced quote costs one line
// of projection rather than blanking the rest of the file — the unbounded
// blast radius #9272 was filed about.
func CommentsOnlyDart(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank(out, 0, len(out))
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
				keep(out, src, i+2, j)
				i = j
			case has(src, i, "/*"):
				state, depth = dartBlock, 1
				i += 2
			case src[i] == 'r' && i+1 < n && (src[i+1] == '\'' || src[i+1] == '"'):
				// Raw-string prefix: the `r` is code, the quote(s) that follow
				// open a raw string (no backslash escapes).
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
				i += 2
			case has(src, i, "*/"):
				depth--
				i += 2
				if depth == 0 {
					state = dartCode
				}
			default:
				keep(out, src, i, i+1)
				i++
			}
		case dartS1, dartD1:
			q := byte('\'')
			if state == dartD1 {
				q = '"'
			}
			switch {
			case !raw && src[i] == '\\':
				i += 2
			case src[i] == q || src[i] == '\n':
				state, raw = dartCode, false
				i++
			default:
				i++
			}
		case dartS3, dartD3:
			q := byte('\'')
			if state == dartD3 {
				q = '"'
			}
			switch {
			case !raw && src[i] == '\\':
				i += 2
			case has(src, i, string([]byte{q, q, q})):
				state, raw = dartCode, false
				i += 3
			default:
				i++
			}
		}
	}
	return out
}

func singleState(q byte) dartState {
	if q == '\'' {
		return dartS1
	}
	return dartD1
}

func tripleState(q byte) dartState {
	if q == '\'' {
		return dartS3
	}
	return dartD3
}

// has reports whether src has literal lit at offset i.
func has(src []byte, i int, lit string) bool {
	if i < 0 || i+len(lit) > len(src) {
		return false
	}
	return string(src[i:i+len(lit)]) == lit
}
