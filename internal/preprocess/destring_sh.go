package preprocess

import "strings"

func init() { Register("destring-sh", DestringSh) }

// DestringSh blanks the contents of single- and double-quoted strings and
// heredoc bodies in shell text; quotes and heredoc delimiter lines survive.
// It is one of the two projections of the shared shell lexer scanSh — this one
// DROPS the data regions, StringsOnlySh keeps only them — so the two can never
// disagree about where a string ends.
//
// It stands in for the validating port's scripts/lib/destring-sh.awk, but is NOT a
// byte-for-byte reimplementation of it — deliberately, because the awk has
// known defects the port exists to remove (issue #6 and its §7.1b): the awk
// reads a heredoc token inside a comment as a real opener and blanks to EOF,
// and its strictly line-based quote matching mis-lexes multi-line constructs.
// This lexer is a proper cross-line lexer: a string, heredoc, or the state
// between them is tracked across newlines. That is what lets it correctly close
// a multi-line single-quoted awk program and see the real `| grep -q` on its
// closing line — a live violation (check-go-golangci-pinned.sh:97) the
// line-based awk blanks and misses. Cross-line tracking is the feature, not an
// accident.
//
// The cost of cross-line tracking is a bounded-blast-radius hazard in ONE
// direction: an unbalanced quote (a lone `"` or `'` with no partner) opens a
// string that runs to the next matching quote or to EOF, blanking real code in
// between. That is the BLIND direction, and it is worth being explicit about
// which way blindness runs here: this projection keeps code and blanks data,
// and a forbidden-pattern rule fires on what SURVIVES, so an over-blank makes
// the blanked text unmatchable and the rule reports a false PASS. An
// over-blank is silence, never a loud false positive. The limitation is shared
// with the awk's own failure modes and is not "fixed" by resetting quote state
// per line, because that reintroduces the awk's multi-line defect and loses the
// :97 find above — the two cannot both be had without a real shell grammar.
// Everywhere the boundary IS decidable, it is decided in favour of keeping
// code: an unquoted backslash escape, an arithmetic `<<`, and a heredoc token
// inside a comment all exist to stop the lexer claiming code as data.
//
// A '#' starts a comment (left untouched to end of line) only at the start of
// input, at the start of a line, or when the previous byte is a space, tab,
// newline, or one of the word-breaking shell metacharacters ';', '|', '&',
// '(', ')', '<', '>' — so ${#var}, $#, and url#fragment are ordinary text,
// while `cmd;#comment` is recognized. Without the comment rule at all, an
// apostrophe in a real comment would open a phantom string and swallow the rest
// of the file. Here-strings (<<<) are not heredocs.
//
// A backslash escapes the next byte both inside double quotes and in UNQUOTED
// text, which is what shell itself does. The unquoted arm is load-bearing, not
// symmetry for its own sake: a bash `[[ =~ ]]` character class routinely
// carries `\"` and `\'` in unquoted text (e.g.
// `[[ "$1" =~ \.go([[:space:]\"\'\/]|$) ]]`), and honoring the escape only
// inside quotes let that `\"` open a phantom string that ran to the next quote
// or to EOF and blanked every comment after it — silent blindness for any rule
// reading this projection, since a blanked comment produces no finding at all.
// An escaped NEWLINE is line joining, so a queued heredoc body begins at the
// next UNESCAPED newline. That is bash: for
//
//	cat <<EOF \
//	body
//	EOF
//
// bash joins the first two lines into `cat <<EOF body`, hands `body` to cat as
// a filename argument (`cat: body: No such file or directory`), and takes the
// `EOF` line as the delimiter of an EMPTY body. So `body` is code and blanking
// it would hide anything written there.
//
// ANSI-C quoting ($'...') is lexed as its own string form because it does not
// share plain single-quote semantics: inside $'...' a backslash IS an escape,
// so $'don\'t' is ONE string. Scanning it with the plain-single-quote arm
// closed on the escaped quote and left the trailing ' to open a phantom string
// that ran to EOF — the same blank-to-EOF blindness the unquoted-backslash fix
// above closed, through a different door.
//
// Heredoc openers (<<WORD, <<-WORD, <<'WORD', <<"WORD", <<\WORD) are
// recognized; the delimiter word must start with a letter or underscore, and a
// << in an ARITHMETIC context is a left shift, never an opener. Both conditions
// are needed: the word-start rule alone covers 1<<20 but not 1<<w, where 'w' is
// a perfectly good delimiter word. Arithmetic context is tracked for all three
// of bash's forms — the $(( … )) expansion, the (( … )) COMMAND, and the rest
// of a `let` simple command — because they evaluate the same grammar and each
// one on its own is a door: `(( mask = 1 << n ))` and `let mask=1<<n` were both
// parsed as heredoc openers. Spans are tracked from the opener to their
// matching )), counting inner parens, so $(( (a+b)<<s )) closes in the right
// place; an arithmetic span is LEXED, not skipped, and an unbalanced ')' closes
// it rather than carrying it to EOF. That distinction is what commit 93983d9
// ("revert the arithmetic skip; it broke more than it fixed") was about: a
// skip loses `((echo "a(b") || true)`, where a quoted paren unbalances the
// count inside nested subshells.
//
// Only a <<- opener strips leading tabs when matching the terminator line; a
// plain << heredoc's terminator must match the body line exactly. The
// remainder of an opener line is lexed normally (its quotes are blanked like
// any other shell text), and multiple openers on one line (the legal
// `cmd <<A <<B` shape) queue their bodies to be blanked in order at the next
// newline.
func DestringSh(src []byte) []byte {
	out := append([]byte(nil), src...)
	for _, s := range scanShData(src) {
		blank(out, s.start, s.end)
	}
	return out
}

// shSpan is a half-open byte range [start, end) of shell source: the interior
// of a quoted string, the body of a heredoc, or a blankable comment.
type shSpan struct{ start, end int }

// scanShData returns the DATA spans of src — string interiors and heredoc
// bodies. It is the projection DestringSh drops and StringsOnlySh keeps, so
// both read the same lexer and cannot disagree about where a string ends.
func scanShData(src []byte) []shSpan {
	data, _ := scanSh(src)
	return data
}

// scanSh is the single shell lexer behind all three shell projections. See
// DestringSh for the grammar it implements.
//
// data is every string interior and heredoc body. comments is every '#'
// comment whose BODY may be blanked, which is a deliberately NARROWER set than
// the comments the lexer merely skips over — see isCommentBlankBoundary. A
// comment outside the narrow set is still skipped (so an apostrophe inside it
// cannot open a phantom string) but never reported here.
func scanSh(src []byte) (data, comments []shSpan) {
	i, n := 0, len(src)
	var pending []heredocOpener
	// arith is the stack of open arithmetic spans; each entry counts the
	// inner '(' still unmatched within that span, so the span closes at the
	// right '))'.
	var arith []int
	// letArith records that a `let` simple command is open: every remaining
	// word on it is an arithmetic expression, so a '<<' is a shift. It ends
	// at the command's end — a newline or one of ';', '|', '&'.
	letArith := false
	flush := func(at int) int {
		for _, h := range pending {
			var s shSpan
			at, s = heredocBodySpan(src, at, h)
			if s.end > s.start {
				data = append(data, s)
			}
		}
		pending = pending[:0]
		return at
	}
	for i < n {
		switch src[i] {
		case '\\':
			// Unquoted backslash: the next byte is literal, so a `\"` or `\'`
			// can never open a string and a `\#` never opens a comment. i+2
			// past the end simply ends the loop, so a trailing backslash is
			// safe. A backslash-NEWLINE is line joining and is swallowed the
			// same way, which is what defers a queued heredoc body to the next
			// UNESCAPED newline — see DestringSh.
			i += 2
		case '#':
			if i == 0 || isWordBreak(src[i-1]) {
				start := i
				for i < n && src[i] != '\n' {
					i++
				}
				// Blanking uses the NARROWER boundary deliberately — see
				// isCommentBlankBoundary. Skipping already happened above.
				if start == 0 || isCommentBlankBoundary(src[start-1]) {
					comments = append(comments, shSpan{start, i})
				}
			} else {
				i++
			}
		case '$':
			if i+2 < n && src[i+1] == '(' && src[i+2] == '(' {
				arith = append(arith, 0)
				i += 3
				continue
			}
			if i+1 < n && src[i+1] == '\'' {
				// ANSI-C quoting: a backslash escapes the next byte, so an
				// escaped quote does not close the string.
				j := i + 2
				for j < n && src[j] != '\'' {
					if src[j] == '\\' && j+1 < n {
						j++
					}
					j++
				}
				data = append(data, shSpan{i + 2, j})
				if j < n {
					j++
				}
				i = j
				continue
			}
			i++
		case '(':
			// `(( expr ))` is bash's arithmetic COMMAND — the same evaluation
			// grammar as `$(( expr ))`, so a '<<' inside it is a left shift.
			// bash itself parses a leading `((` this way, so reading it as an
			// arithmetic span rather than two subshells matches the shell.
			if i+1 < n && src[i+1] == '(' {
				arith = append(arith, 0)
				i += 2
				continue
			}
			if len(arith) > 0 {
				arith[len(arith)-1]++
			}
			i++
		case ')':
			if len(arith) > 0 {
				last := len(arith) - 1
				if arith[last] > 0 {
					arith[last]--
					i++
					continue
				}
				if i+1 < n && src[i+1] == ')' {
					arith = arith[:last]
					i += 2
					continue
				}
				// Unbalanced ')' inside an arithmetic span: the `((` was really
				// nested subshells, so close the span rather than carry it to
				// EOF. This is why the span is tracked and not skipped.
				arith = arith[:last]
			}
			i++
		case ';', '|', '&':
			letArith = false
			i++
		case '\'':
			j := i + 1
			for j < n && src[j] != '\'' {
				j++
			}
			data = append(data, shSpan{i + 1, j})
			if j < n {
				j++
			}
			i = j
		case '"':
			j := i + 1
			for j < n && src[j] != '"' {
				if src[j] == '\\' && j+1 < n {
					j++
				}
				j++
			}
			data = append(data, shSpan{i + 1, j})
			if j < n {
				j++
			}
			i = j
		case '<':
			// In an arithmetic context a '<<' is a left shift; the operand
			// after it is an expression term, not a heredoc delimiter word.
			if len(arith) == 0 && !letArith && i+1 < n && src[i+1] == '<' && !(i+2 < n && src[i+2] == '<') {
				if h, end, ok := parseHeredocOpener(src, i); ok {
					pending = append(pending, h)
					i = end
					continue
				}
			}
			// Advance past the whole run of consecutive '<' (e.g. the full
			// <<< of a here-string) so no later '<' in the run can be
			// re-examined as the start of a heredoc opener.
			for i < n && src[i] == '<' {
				i++
			}
		case '\n':
			letArith = false
			i++
			if len(pending) > 0 {
				i = flush(i)
			}
		default:
			if !letArith && isLetKeyword(src, i) {
				letArith = true
				i += 3
				continue
			}
			i++
		}
	}
	// A heredoc opener with no trailing newline (opener line runs to EOF)
	// leaves its body queued but never triggered by the '\n' case above.
	if len(pending) > 0 {
		flush(i)
	}
	return data, comments
}

// heredocOpener records one << (or <<-) opener's delimiter word and whether
// it strips leading tabs (<<-) when matching the terminator line.
type heredocOpener struct {
	word     string
	dashFlag bool
}

// parseHeredocOpener parses the heredoc opener token starting at the << found
// at src[at]: an optional '-' (the <<- tab-stripping flag), optional
// whitespace, an optional single leading quote character (' or ") or
// backslash, then the delimiter word, then — for a quoted opener — the
// matching closing quote. The delimiter word must start with a letter or
// underscore. end is the offset just past the opener token, ready for normal
// lexing to resume on the rest of the line. ok is false when no valid
// delimiter word follows (e.g. 1<<20, where '2' cannot start a word).
func parseHeredocOpener(src []byte, at int) (h heredocOpener, end int, ok bool) {
	n := len(src)
	j := at + 2
	dash := false
	if j < n && src[j] == '-' {
		dash = true
		j++
	}
	for j < n && (src[j] == ' ' || src[j] == '\t') {
		j++
	}
	var quote byte
	if j < n && (src[j] == '\'' || src[j] == '"' || src[j] == '\\') {
		quote = src[j]
		j++
	}
	ws := j
	if j >= n || !isHeredocWordStart(src[j]) {
		return heredocOpener{}, 0, false
	}
	for j < n && isHeredocWordChar(src[j]) {
		j++
	}
	word := string(src[ws:j])
	// A quoted delimiter (' or ") has a matching closer to consume so the
	// rest-of-line lexer doesn't re-open it as a fresh string. A leading
	// backslash (<<\WORD) has no closing counterpart.
	if (quote == '\'' || quote == '"') && j < n && src[j] == quote {
		j++
	}
	return heredocOpener{word: word, dashFlag: dash}, j, true
}

// heredocBodySpan locates the body of the heredoc opened by h, whose first
// byte is src[body]. It returns the offset at which normal lexing resumes and
// the body's data span.
//
// With NO terminator line there is no body: the span is empty and lexing
// resumes as normal shell at the line after the opener.
//
// bash would take every remaining line as the body ("here-document delimited
// by end-of-file"), and an earlier version of this function matched that. It
// is the wrong trade for a projection whose consumers are gates. A file that
// reaches this path is malformed shell, and the two ways of handling it are
// not symmetric:
//
//   - Blanking to EOF is SILENT. Everything below the stray opener becomes
//     unmatchable, so every rule reading this projection passes a file it
//     should fail — and a single unpaired `<<WORD` anywhere in a file is then
//     enough to switch the gate off for the rest of it.
//   - Reading it as shell can only ever produce a LOUD false positive, which
//     the next reader corrects.
//
// Fail-closed means preferring the loud direction. It is also what the rest of
// this family already does: an unterminated quote is closed at the newline
// rather than swallowing the file, and the comments-only lexers bound their
// unterminated literals the same way. Blanking to EOF was the one place that
// disagreed.
func heredocBodySpan(src []byte, body int, h heredocOpener) (resume int, s shSpan) {
	n := len(src)
	for k := body; k < n; {
		lineEnd := k
		for lineEnd < n && src[lineEnd] != '\n' {
			lineEnd++
		}
		line := string(src[k:lineEnd])
		if h.dashFlag {
			line = strings.TrimLeft(line, "\t")
		}
		if line == h.word {
			if lineEnd < n {
				lineEnd++
			}
			return lineEnd, shSpan{body, k}
		}
		if lineEnd < n {
			lineEnd++
		}
		k = lineEnd
	}
	// No terminator: not a heredoc. Resume lexing at the body's first line so
	// its contents stay readable code.
	return body, shSpan{body, body}
}

// isWordBreak reports whether byte c ends a shell word. It answers two
// questions with one table: whether a '#' immediately after c starts a
// comment rather than being glued to the previous token (as in ${#pw}, $#, or
// url#fragment), and whether a following `let` sits at the head of a simple
// command. Whitespace and the word-breaking shell operator characters ';',
// '|', '&', '(', ')', '<', '>' all qualify.
func isWordBreak(c byte) bool {
	switch c {
	case ' ', '\t', '\n', ';', '|', '&', '(', ')', '<', '>':
		return true
	default:
		return false
	}
}

// isCommentBlankBoundary reports whether a comment starting after byte c may
// have its BODY blanked, as opposed to merely being skipped.
//
// The two boundaries differ on purpose, and the difference is a fail-safe
// choice. Skipping uses the wide isWordBreak set so an apostrophe in
// `cmd;#don't` cannot open a phantom string — being wrong there costs nothing,
// since the text is left exactly as it was. Blanking DESTROYS text, so being
// wrong there hides real code from every rule that reads the variant, which is
// the missed-violation direction the exit-code contract exists to prevent. It
// therefore matches the reference pipeline's stripper,
// `sed -E 's/(^|[[:space:]])#.*$//'`: start-of-line, space or tab only. In
// bash `x=$(ls)#tag` is a single word, so blanking on the ')' boundary
// erased live code — including any violation sharing the line.
func isCommentBlankBoundary(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n'
}

// isLetKeyword reports whether src[i:] begins the `let` builtin at the head of
// a word, with at least one argument to follow. Every argument of `let` is an
// arithmetic expression, so `let mask=1<<n` shifts — it does not open a
// heredoc named `n`.
func isLetKeyword(src []byte, i int) bool {
	if src[i] != 'l' || i+4 > len(src) {
		return false
	}
	if i > 0 && !isWordBreak(src[i-1]) {
		return false
	}
	return src[i+1] == 'e' && src[i+2] == 't' && (src[i+3] == ' ' || src[i+3] == '\t')
}

func isHeredocWordChar(c byte) bool {
	return c == '_' || c == '-' ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

// isHeredocWordStart reports whether c may be the first character of a
// heredoc delimiter word. Only letters and underscore qualify — not digits,
// and not '-' (which after << is always the <<- flag, never part of the
// word) — so a shift by a numeric literal, 1<<20, is never parsed as an
// opener. A shift by a NAMED variable, 1<<w, passes this test (w is a legal
// delimiter word) and is excluded by scanSh's arithmetic-context tracking
// instead; this predicate cannot tell the two apart on its own.
func isHeredocWordStart(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}
