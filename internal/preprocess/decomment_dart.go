package preprocess

func init() { Register("decomment-dart", DecommentDart) }

// dartCommentFrame is either code (quote == 0) or a string. An interpolation
// code frame has a positive brace depth; the outermost code frame has none.
type dartCommentFrame struct {
	quote  byte
	width  int
	raw    bool
	braces int
}

// DecommentDart blanks Dart comments, retaining every other byte, including
// literal text and executable interpolation. Nested comments, raw/triple
// strings and nested interpolation are lexical contexts, not regex boundaries.
// Output owns its storage and preserves byte offsets and CR/LF positions.
// Unterminated block comments run to EOF; a non-triple string ends at a bare
// CR or LF, bounding malformed source without truncating subsequent code.
func DecommentDart(src []byte) []byte {
	out := append([]byte(nil), src...)
	frames := []dartCommentFrame{{}}
	for i := 0; i < len(src); {
		frame := &frames[len(frames)-1]
		if frame.quote != 0 {
			switch {
			case !frame.raw && src[i] == '\\':
				i = min(i+2, len(src))
			case !frame.raw && has(src, i, "${"):
				frames = append(frames, dartCommentFrame{braces: 1})
				i += 2
			case dartCommentQuote(src, i, frame.quote, frame.width):
				i += frame.width
				frames = frames[:len(frames)-1]
			case frame.width == 1 && dartCommentNewline(src[i]):
				frames = frames[:len(frames)-1]
				i++
			default:
				i++
			}
			continue
		}

		switch {
		case has(src, i, "//"):
			start := i
			for i < len(src) && !dartCommentNewline(src[i]) {
				i++
			}
			blankDartComment(out, start, i)
		case has(src, i, "/*"):
			start, depth := i, 1
			i += 2
			for i < len(src) && depth > 0 {
				switch {
				case has(src, i, "/*"):
					depth++
					i += 2
				case has(src, i, "*/"):
					depth--
					i += 2
				default:
					i++
				}
			}
			blankDartComment(out, start, i)
		case frame.braces > 0 && src[i] == '{':
			frame.braces++
			i++
		case frame.braces > 0 && src[i] == '}':
			frame.braces--
			if frame.braces == 0 {
				frames = frames[:len(frames)-1]
			}
			i++
		default:
			raw := src[i] == 'r' && i+1 < len(src) && (src[i+1] == '\'' || src[i+1] == '"')
			if raw {
				i++
			}
			if src[i] == '\'' || src[i] == '"' {
				width := 1
				if dartCommentQuote(src, i, src[i], 3) {
					width = 3
				}
				frames = append(frames, dartCommentFrame{quote: src[i], width: width, raw: raw})
				i += width
			} else {
				i++
			}
		}
	}
	return out
}

func dartCommentNewline(b byte) bool { return b == '\n' || b == '\r' }

// Unlike Go's blank, Dart's projection preserves standalone CR line endings.
func blankDartComment(dst []byte, start, end int) {
	for i := start; i < end; i++ {
		if !dartCommentNewline(dst[i]) {
			dst[i] = ' '
		}
	}
}

func dartCommentQuote(src []byte, i int, quote byte, width int) bool {
	return src[i] == quote && (width == 1 ||
		i+2 < len(src) && src[i+1] == quote && src[i+2] == quote)
}
