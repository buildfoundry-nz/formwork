package preprocess

// PostgreSQL comment lexing, defined once.
//
// Two transforms need opposite halves of the same answer — CommentsOnlySQL
// keeps comment contents and blanks everything else, DecommentSQL blanks
// comments and keeps everything else — and the hard part is identical for both:
// deciding what is NOT a comment. A `--` inside a string literal is row data; a
// dollar-quoted function body is a string literal, so a `--` inside one is
// content rather than a comment. Written twice, those rules drift; the second
// copy is the one that misses a case and reads as a pass.

// sqlState is the lexer state of sqlScanComments.
type sqlState int

const (
	sqlCode   sqlState = iota
	sqlBlock           // /* … */, NESTS in PostgreSQL
	sqlSQ              // '…' or E'…', may span lines
	sqlDQ              // "…" quoted identifier, may span lines
	sqlDollar          // $tag$ … $tag$, may span lines
)

// sqlScanComments walks src under PostgreSQL lexical rules and reports every
// comment twice over: onRegion receives each maximal comment span INCLUDING its
// delimiters, onBody receives the runs inside one that are content rather than
// delimiter (the nested `/*` and `*/` markers of a nesting block comment are
// not body). Either callback may be nil. Spans are half-open byte ranges into
// src, emitted in source order, and empty spans are never emitted.
//
// PostgreSQL lexical notes honoured here:
//   - `--` runs to end of line; `/* … */` NEST.
//   - `'…'`: a doubled single quote stays in the string; backslashes are literal
//     UNLESS the string is an escape string `E'…'`/`e'…'`, where `\` escapes.
//   - `"…"`: `""` is an escaped quote. Not a comment.
//   - `$tag$…$tag$`: literal with no escapes; the tag is empty (`$$`) or an
//     identifier that cannot start with a digit, so `$1` positional parameters
//     are not openers. A `$` glued onto a preceding identifier character
//     continues that identifier (`col$a$name` is ONE identifier) and opens
//     nothing — the same adjacency guard the E-string opener applies.
//
// Unterminated constructs are read the way the text reads: an unterminated
// comment runs to end of input, and an unterminated string swallows the rest,
// so no comment is found inside it. Callers that hand this function a FRAGMENT
// of a larger statement (sqlextract's partial reassembly) get that behaviour
// deliberately — it is the fail-closed direction for a `require`, which cannot
// then be satisfied by text the caller could not prove is code.
func sqlScanComments(src []byte, onRegion, onBody func(start, end int)) {
	emit := func(f func(int, int), start, end int) {
		if f != nil && start < end {
			f(start, end)
		}
	}
	n := len(src)
	state := sqlCode
	depth, esc := 0, false
	var tag string
	regionStart, bodyStart := 0, 0
	for i := 0; i < n; {
		switch state {
		case sqlCode:
			switch {
			case has(src, i, "--"):
				j := i + 2
				for j < n && src[j] != '\n' {
					j++
				}
				emit(onBody, i+2, j)
				emit(onRegion, i, j)
				i = j
			case has(src, i, "/*"):
				state, depth = sqlBlock, 1
				regionStart = i
				i += 2
				bodyStart = i
			case src[i] == '$' && !sqlIdentByte(prevByte(src, i)):
				if dt, ok := dollarTag(src, i); ok {
					state, tag = sqlDollar, dt
					i += len(dt)
					continue
				}
				i++
			case (src[i] == 'E' || src[i] == 'e') && has(src, i+1, "'") && !sqlIdentByte(prevByte(src, i)):
				state, esc = sqlSQ, true
				i += 2
			case src[i] == '\'':
				state, esc = sqlSQ, false
				i++
			case src[i] == '"':
				state = sqlDQ
				i++
			default:
				i++
			}
		case sqlBlock:
			switch {
			case has(src, i, "/*"):
				emit(onBody, bodyStart, i)
				depth++
				i += 2
				bodyStart = i
			case has(src, i, "*/"):
				emit(onBody, bodyStart, i)
				depth--
				i += 2
				bodyStart = i
				if depth == 0 {
					emit(onRegion, regionStart, i)
					state = sqlCode
				}
			default:
				i++
			}
		case sqlSQ:
			switch {
			case esc && src[i] == '\\':
				i += 2
			case src[i] == '\'':
				if has(src, i+1, "'") { // '' — an escaped quote, still inside
					i += 2
					continue
				}
				state, esc = sqlCode, false
				i++
			default:
				i++
			}
		case sqlDQ:
			switch {
			case src[i] == '"':
				if has(src, i+1, `"`) { // "" — an escaped quote, still inside
					i += 2
					continue
				}
				state = sqlCode
				i++
			default:
				i++
			}
		case sqlDollar:
			if has(src, i, tag) {
				i += len(tag)
				state, tag = sqlCode, ""
				continue
			}
			i++
		}
	}
	// An unterminated block comment is a comment to end of input. Flushed here
	// rather than inside the loop so the terminated and unterminated paths agree
	// on what "body" means.
	if state == sqlBlock {
		emit(onBody, bodyStart, n)
		emit(onRegion, regionStart, n)
	}
}

// dollarTag returns the full dollar-quote delimiter opening at src[at] (which
// must be '$') — "$$" for an empty tag, or "$name$" — and whether src[at] opens
// one at all. A non-empty tag follows unquoted-identifier rules: a leading
// letter or underscore, then letters, digits or underscores.
func dollarTag(src []byte, at int) (string, bool) {
	j := at + 1
	if j < len(src) && sqlIdentStartByte(src[j]) {
		for j < len(src) && sqlIdentByte(src[j]) {
			j++
		}
	}
	if j < len(src) && src[j] == '$' {
		return string(src[at : j+1]), true
	}
	return "", false
}

// prevByte returns the byte before offset i, or 0 at the start of input (which
// is not an identifier byte, so a leading '$' or 'E' is at a token boundary).
func prevByte(src []byte, i int) byte {
	if i <= 0 {
		return 0
	}
	return src[i-1]
}

func sqlIdentByte(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

func sqlIdentStartByte(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}
