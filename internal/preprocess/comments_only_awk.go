package preprocess

func init() { Register("comments-only-awk", CommentsOnlyAwk) }

// CommentsOnlyAwk keeps only the CONTENTS of awk '#' comments: the text after
// each '#' survives verbatim; code, "…" string contents, /…/ regex-literal
// contents, and every delimiter (the '#' included) become spaces. Same
// contract and same purpose as CommentsOnlyGo; see that doc comment for why a
// comment-opener regex cannot substitute.
//
// awk is NOT a scope widening of the shell lexer, and the difference is not
// cosmetic:
//
//   - awk has /regex/ literals. A '#' inside one is regex content. Whether a
//     '/' opens a regex or is the division operator cannot be decided from the
//     byte alone — it depends on what came before, because a regex literal is
//     only valid where an OPERAND is expected. prevValue below carries exactly
//     that one bit.
//   - awk has no heredocs and no single-quoted strings. The shell lexer treats
//     an apostrophe as opening a literal that spans lines, so it desyncs on the
//     first apostrophe in awk code or in a comment ("don't") and blanks every
//     comment after it — a silent pass, not a visible error.
//
// Both failure directions are real, but they are not equally bad. Reading a
// regex as division makes a '#' inside the regex look like a comment opener,
// which reports a violation that is not there — loud, and corrected by the
// next reader. Reading division as a regex swallows the rest of the line
// hunting a closing '/', which blanks a real comment and makes any marker in
// it unmatchable — silent, and it passes a file that should fail. Where the
// grammar is genuinely ambiguous this lexer therefore prefers division.
func CommentsOnlyAwk(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank(out, 0, len(out))
	n := len(src)

	// prevValue reports whether the last significant token can END an
	// expression. If it can, a following '/' is division; if it cannot, the
	// '/' opens a regex literal. This is the whole of awk's regex/division
	// disambiguation.
	prevValue := false

	// ctrlParen is a stack, one entry per open '(', recording whether that
	// paren opened an if/while/for CONDITION. A ')' normally ends a value, but
	// the ')' closing a condition is followed by a STATEMENT, where `/re/` is a
	// regex pattern rather than a division. Without this, `if (x) /re/` mislexes.
	var ctrlParen []bool

	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '#':
			j := i + 1
			for j < n && src[j] != '\n' {
				j++
			}
			keep(out, src, i+1, j)
			i = j
			prevValue = false

		case c == '\n':
			// A newline terminates an awk statement, so the next '/' is in
			// operand position.
			prevValue = false
			i++

		case c == '\\' && i+1 < n && src[i+1] == '\n':
			// Line continuation: the statement is not over, so prevValue stands.
			i += 2

		case c == ' ' || c == '\t' || c == '\r':
			i++

		case c == '"':
			i = skipAwkString(src, i)
			prevValue = true

		case c == '/':
			if prevValue {
				// Division (or '/='). An operator, so an operand is expected next.
				if i+1 < n && src[i+1] == '=' {
					i += 2
				} else {
					i++
				}
				prevValue = false
				break
			}
			i = skipAwkRegex(src, i)
			// A regex literal is a value: it evaluates to a match against $0.
			prevValue = true

		case isAwkIdentStart(c):
			j := i
			for j < n && isAwkIdentByte(src[j]) {
				j++
			}
			word := string(src[i:j])
			// A statement keyword does not produce a value, so a '/' after it
			// opens a regex (`print /re/`). Every other identifier — including
			// bare `length` and `getline` — can end an expression.
			prevValue = !awkStatementKeyword(word)
			// Remember whether the paren that follows opens a CONDITION.
			if awkCondKeyword(word) {
				k := j
				for k < n && (src[k] == ' ' || src[k] == '\t') {
					k++
				}
				if k < n && src[k] == '(' {
					ctrlParen = append(ctrlParen, true)
					prevValue = false
					j = k + 1
				}
			}
			i = j

		case c >= '0' && c <= '9':
			j := i
			for j < n && isAwkNumByte(src[j]) {
				j++
			}
			prevValue = true
			i = j

		case c == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9':
			j := i + 1
			for j < n && isAwkNumByte(src[j]) {
				j++
			}
			prevValue = true
			i = j

		case c == '(':
			// Not a condition paren (those are consumed with their keyword).
			ctrlParen = append(ctrlParen, false)
			prevValue = false
			i++

		case c == ')':
			cond := false
			if len(ctrlParen) > 0 {
				cond = ctrlParen[len(ctrlParen)-1]
				ctrlParen = ctrlParen[:len(ctrlParen)-1]
			}
			// After a condition's ')' a statement follows, so an operand is
			// expected; after any other ')' the value is complete.
			prevValue = !cond
			i++

		case c == ']':
			prevValue = true
			i++

		case c == '$':
			// A field reference: '$' itself is not a value, the operand after
			// it is, and that operand is lexed by the arms above.
			prevValue = false
			i++

		case c == '+' || c == '-':
			// Postfix ++/-- leaves a value; every other use is an operator, so
			// an operand is expected next.
			if i+1 < n && src[i+1] == c && prevValue {
				i += 2
				break
			}
			prevValue = false
			i++

		default:
			// Every other byte is an operator, separator or brace: an operand
			// is expected after it.
			prevValue = false
			i++
		}
	}
	return out
}

// skipAwkString returns the offset just past the double-quoted string opening
// at src[at]. awk has no single-quoted strings and a string cannot span lines,
// so an unterminated one is closed at the newline — bounding the damage to one
// line rather than blanking the rest of the file.
func skipAwkString(src []byte, at int) int {
	n := len(src)
	for i := at + 1; i < n; {
		switch src[i] {
		case '\\':
			if i+1 < n && src[i+1] == '\n' {
				return i // Unterminated: leave the newline to the caller.
			}
			i += 2
		case '"':
			return i + 1
		case '\n':
			return i
		default:
			i++
		}
	}
	return n
}

// skipAwkRegex returns the offset just past the /…/ regex literal opening at
// src[at]. A '/' inside a bracket expression is literal and does not close the
// literal, and a backslash escapes the next byte. An unterminated regex is
// closed at the newline, for the same reason as a string.
func skipAwkRegex(src []byte, at int) int {
	n := len(src)
	inBracket := false
	for i := at + 1; i < n; {
		c := src[i]
		switch {
		case c == '\\':
			if i+1 < n && src[i+1] == '\n' {
				return i
			}
			i += 2
		case c == '\n':
			return i
		case inBracket:
			if c == ']' {
				inBracket = false
			}
			i++
		case c == '[':
			inBracket = true
			i++
			// A '^' negates, and a ']' in the FIRST position of a bracket
			// expression is a literal ']' rather than the closing delimiter.
			if i < n && src[i] == '^' {
				i++
			}
			if i < n && src[i] == ']' {
				i++
			}
		case c == '/':
			return i + 1
		default:
			i++
		}
	}
	return n
}

// awkStatementKeyword reports whether word is a keyword that cannot end an
// expression, so a '/' immediately after it opens a regex literal rather than
// being the division operator.
func awkStatementKeyword(word string) bool {
	switch word {
	case "BEGIN", "END", "function", "func",
		"if", "else", "while", "for", "do",
		"break", "continue", "next", "nextfile", "exit", "return",
		"delete", "in", "print", "printf", "case", "default":
		return true
	}
	return false
}

// awkCondKeyword reports whether word introduces a parenthesised CONDITION,
// after whose closing ')' a statement — and therefore a possible regex
// literal — follows rather than a continuation of an expression.
func awkCondKeyword(word string) bool {
	return word == "if" || word == "while" || word == "for"
}

func isAwkIdentStart(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isAwkIdentByte(c byte) bool {
	return isAwkIdentStart(c) || ('0' <= c && c <= '9')
}

// isAwkNumByte accepts the bytes that can continue a numeric constant,
// including the hex and exponent forms.
func isAwkNumByte(c byte) bool {
	return ('0' <= c && c <= '9') || c == '.' ||
		('a' <= c && c <= 'f') || ('A' <= c && c <= 'F') ||
		c == 'x' || c == 'X'
}
