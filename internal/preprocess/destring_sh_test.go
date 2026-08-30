package preprocess

import (
	"strings"
	"testing"
)

func TestDestringSh(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"single-quoted interior blanked, quotes kept": {
			in:   "echo 'secret pw'\n",
			want: "echo '" + sp("secret pw") + "'\n",
		},
		// Cross-line, escape-honoring (as it has always been): `\"` inside a
		// double-quoted string does not close it, so the whole interior `a\"b`
		// is blanked.
		"double-quoted interior blanked with escapes": {
			in:   `echo "a\"b"` + "\n",
			want: `echo "` + sp(`a\"b`) + `"` + "\n",
		},
		// Cross-line by design: an unterminated quote runs to EOF, blanking the
		// intervening lines. This is the bounded-blast-radius limitation the
		// DestringSh doc calls out — accepted because cross-line tracking is
		// what finds violations hidden after a multi-line quote (the awk, being
		// line-based, both mis-lexes those AND cannot bleed like this).
		"unterminated double quote blanked to EOF": {
			in:   "echo \"open\nmore\n",
			want: "echo \"" + sp("open") + "\n" + sp("more") + "\n",
		},
		"apostrophe in comment does not open a string": {
			in:   "# don't trip\necho 'x'\n",
			want: "# don't trip\necho '" + sp("x") + "'\n",
		},
		"heredoc body blanked, delimiters kept": {
			in:   "cat <<EOF\nsecret line\nEOF\necho done\n",
			want: "cat <<EOF\n" + sp("secret line") + "\nEOF\necho done\n",
		},
		"dash heredoc with tab-indented delimiter": {
			in:   "cat <<-EOF\n\tbody\n\tEOF\nafter\n",
			want: "cat <<-EOF\n" + sp("\tbody") + "\n\tEOF\nafter\n",
		},
		"quoted heredoc delimiter": {
			in:   "cat <<'EOF'\nbody\nEOF\n",
			want: "cat <<'EOF'\n" + sp("body") + "\nEOF\n",
		},
		"here-string is not a heredoc": {
			in:   "cat <<< word\n",
			want: "cat <<< word\n",
		},
		"here-string with trailing content stays untouched": {
			in:   "cat <<< word\nsecret\nmore\n",
			want: "cat <<< word\nsecret\nmore\n",
		},
		// Degenerate case: four consecutive '<' is not valid shell syntax
		// (not a heredoc, not a here-string), but the guard treats it the
		// same as a here-string run and skips past all four '<' without
		// touching anything after it. Stable and harmless for our purposes.
		"four consecutive < is left untouched": {
			in:   "a <<<< b\n",
			want: "a <<<< b\n",
		},
		// CHANGED EXPECTATION (was "unterminated heredoc blanks to EOF", want
		// "cat <<EOF\n" + sp("never ends") + "\n").
		//
		// A heredoc whose terminator never arrives is not a heredoc. bash would
		// take the rest of the file as the body, but this projection feeds
		// gates, and the two readings fail in opposite directions: blanking to
		// EOF is SILENT (everything below the stray opener stops matching, so
		// one unpaired `<<WORD` switches a rule off for the rest of the file),
		// while reading it as shell can only over-report, which is LOUD and
		// self-correcting. Fail-closed takes the loud one. It also matches the
		// rest of the family — an unterminated quote is closed at the newline,
		// not at EOF.
		"an unterminated heredoc is not a heredoc; its body stays readable": {
			in:   "cat <<EOF\nnever ends\n",
			want: "cat <<EOF\nnever ends\n",
		},
		"empty input": {in: "", want: ""},
		// CHANGED EXPECTATION (was "unquoted backslash-apostrophe opens a
		// phantom string (over-blanking)", want
		// "echo don\\'   \n     'y'\n"). The old case name conceded in its own
		// title that the output was wrong: it pinned a defect, not a behaviour.
		//
		// Shell semantics: OUTSIDE quotes a backslash escapes the NEXT byte, so
		// `don\'t` carries a literal apostrophe and opens no string at all. The
		// old reading opened a phantom string that ran (cross-line) to the
		// apostrophe of `'y'` and blanked the real code between. That is the
		// MISSED-VIOLATION direction, not a cosmetic over-blank: a blanked line
		// produces no finding, so every rule reading this projection goes
		// silently blind past a stray `\'`. The live shape is an ordinary bash
		// `[[ =~ ]]` character class —
		// `[[ "$1" =~ \.go([[:space:]\"\'\/]|$) ]]` — which is why the escape
		// arm is load-bearing rather than symmetry for its own sake.
		"unquoted backslash escapes the apostrophe, so no string opens": {
			in:   "echo don\\'t x\necho 'y'\n",
			want: "echo don\\'t x\necho '" + sp("y") + "'\n",
		},
		// $'...' (ANSI-C quoting) is its own string form, because it does not
		// share plain-single-quote semantics: inside $'...' a backslash IS an
		// escape, so $'don\'t' is ONE string. Scanning it with the plain
		// single-quote arm closed on the escaped quote and left the trailing '
		// to open a phantom string that ran to EOF. The projection for a plain
		// body like this one is unchanged — interior blanked, quotes kept.
		"$'...' ansi-c quoting blanks the interior, quotes kept": {
			in:   "x=$'foo'\n",
			want: "x=$'" + sp("foo") + "'\n",
		},
		// A1: heredoc delimiter word must start with [A-Za-z_]; digits (and
		// '-', the <<- flag) are not valid first characters, so an
		// arithmetic left-shift like $((1<<20)) is never mistaken for a
		// heredoc opener.
		"A1 repro: $((1<<20)) is arithmetic, not a heredoc": {
			in:   "x=$((1<<20))\n",
			want: "x=$((1<<20))\n",
		},
		"A1 adversarial: $((1<<20)) followed by a real quoted string on a later line": {
			in:   "x=$((1<<20))\necho \"forbidden_secret\"\n",
			want: "x=$((1<<20))\necho \"" + sp("forbidden_secret") + "\"\n",
		},
		// A2: a leading backslash before the delimiter word (<<\EOF, POSIX
		// shorthand for <<'EOF') must be recognized so the body is blanked
		// verbatim instead of being lexed as shell text (where an apostrophe
		// in the body would open a phantom string).
		"A2 repro: backslash-escaped heredoc delimiter <<\\EOF is recognized": {
			in:   "cat <<\\EOF\nbody\nEOF\n",
			want: "cat <<\\EOF\n" + sp("body") + "\nEOF\n",
		},
		"A2 adversarial: apostrophe in body, quoted secret after terminator": {
			in:   "cat <<\\EOF\nit's\nEOF\necho 'safe'\n",
			want: "cat <<\\EOF\n" + sp("it's") + "\nEOF\necho '" + sp("safe") + "'\n",
		},
		// A3: '#' starts a comment only at start-of-input/line or when
		// preceded by space/tab — matching shell word-boundary rules — so
		// ${#pw}, $#, and url#fragment are ordinary text, not comment starts.
		"A3 repro: ${#pw} and $# are not comment starts": {
			in:   "n=$# echo 'a'\nu=url#frag echo 'b'\n",
			want: "n=$# echo '" + sp("a") + "'\nu=url#frag echo '" + sp("b") + "'\n",
		},
		"A3 adversarial: ${#pw} followed by a quoted string on the same line": {
			in:   "x=${#pw} echo \"secret\"\n",
			want: "x=${#pw} echo \"" + sp("secret") + "\"\n",
		},
		// A4: only a <<- opener strips leading tabs before comparing a body
		// line to the delimiter word; a plain <<EOF heredoc's terminator
		// must match the line exactly, so a tab-indented "\tEOF" body line
		// does not end it early.
		"A4: plain heredoc does not terminate early on tab-indented delimiter line": {
			in:   "cat <<EOF\n\tEOF\nreal secret\nEOF\n",
			want: "cat <<EOF\n" + sp("\tEOF") + "\n" + sp("real secret") + "\nEOF\n",
		},
		// A5: the remainder of the heredoc opener line is lexed normally
		// (quotes blanked) instead of being skipped wholesale; multiple
		// openers on one line (`cat <<A <<B`) queue their bodies and blank
		// them in order at the next newline.
		"A5 repro: heredoc opener line remainder is destringed": {
			in:   "cat <<EOF | grep 'forbidden_secret'\nbody\nEOF\n",
			want: "cat <<EOF | grep '" + sp("forbidden_secret") + "'\n" + sp("body") + "\nEOF\n",
		},
		"A5 adversarial: two heredocs on one line, both bodies blanked in order": {
			in:   "cat <<A <<B\nbodyA\nA\nbodyB\nB\n",
			want: "cat <<A <<B\n" + sp("bodyA") + "\nA\n" + sp("bodyB") + "\nB\n",
		},
		// Finding 1: shell word-breaking metacharacters (; | & ( ) < >) also
		// end a word, so '#' right after one of them starts a comment, same
		// as after space/tab/newline/start-of-input. Without this, the
		// apostrophe in "it's" inside an unrecognized comment opens a
		// phantom string that swallows real code — including a later quoted
		// secret — as ordinary (unblanked) text.
		"finding1 repro: ; before # starts a comment, forbidden_secret still blanked": {
			in:   "echo hi;#comment it's broken\nx=1\necho 'forbidden_secret'\n",
			want: "echo hi;#comment it's broken\nx=1\necho '" + sp("forbidden_secret") + "'\n",
		},
		"finding1: | before # starts a comment": {
			in:   "echo hi|#c it's\necho 'x'\n",
			want: "echo hi|#c it's\necho '" + sp("x") + "'\n",
		},
		"finding1: & before # starts a comment": {
			in:   "true&#c don't\necho 'y'\n",
			want: "true&#c don't\necho '" + sp("y") + "'\n",
		},
		"finding1: ( before # starts a comment": {
			in:   "f(#c isn't\necho 'z'\n",
			want: "f(#c isn't\necho '" + sp("z") + "'\n",
		},
		"finding1: ) before # starts a comment": {
			in:   "f()#c can't\necho 'z'\n",
			want: "f()#c can't\necho '" + sp("z") + "'\n",
		},
		"finding1: < before # starts a comment": {
			in:   "a <#c wasn't\necho 'z'\n",
			want: "a <#c wasn't\necho '" + sp("z") + "'\n",
		},
		"finding1: > before # starts a comment": {
			in:   "a >#c hasn't\necho 'z'\n",
			want: "a >#c hasn't\necho '" + sp("z") + "'\n",
		},
		// `((` is ambiguous at the byte level: an arithmetic command `(( x ))`
		// versus nested subshells `(( ... ) ...)`. Commit 93983d9 ("revert the
		// arithmetic skip; it broke more than it fixed") backed out a revision
		// that PAREN-SKIPPED every `((` — skipping meant a quoted paren inside a
		// nested subshell unbalanced the count and swallowed the rest of the
		// file. The merged lexer does not skip: it opens an arithmetic SPAN,
		// keeps lexing normally inside it (strings are still strings), and, on a
		// ')' that is not part of a closing '))', closes the span rather than
		// carrying it to EOF. That recovery is what makes this case pass while
		// the case below also passes — the two together are the discriminator
		// between "tracked" and "skipped".
		"nested subshell with a paren inside a string is lexed, not skipped": {
			in:   "((echo \"a(b\") || true)\necho 'forbidden_secret'\n",
			want: "((echo \"" + sp("a(b") + "\") || true)\necho '" + sp("forbidden_secret") + "'\n",
		},
		// CHANGED EXPECTATION (was "parity: $((1<<n)) blanks to EOF, matching
		// the reference awk", want "mask=$((1<<n))\n" + sp("echo
		// 'forbidden_secret'") + "\n").
		//
		// `1<<n` inside $(( )) is a left SHIFT. Reading `n` as a heredoc
		// delimiter word made the body blanker eat every line to EOF hunting a
		// terminator that cannot exist — so `forbidden_secret` and every comment
		// after it vanished, and the rules reading this projection reported OK.
		// Matching the reference awk's blindness was never worth having: the awk
		// is the thing this port exists to replace, and the DestringSh doc
		// already lists two other places where it deliberately diverges from it
		// because the awk is wrong.
		//
		// Why re-landing is safe THIS time. 93983d9 reverted an earlier attempt
		// because it implemented arithmetic as a paren SKIP, which lost the
		// nested-subshell case directly above. The merged lexer tracks
		// arithmetic as a span it still lexes inside, with an unbalanced-')'
		// recovery — so the two cases coexist, and the whole suite (this table,
		// TestDestringDecommentSh, the three ported fork tables below, and the
		// StringsOnlySh complement proof) is green together. The nested-subshell
		// case is the standing guard: if a future revision reaches for the skip
		// again, that case fails.
		"$((1<<n)) is a shift by a variable, not a heredoc opener": {
			in:   "mask=$((1<<n))\necho 'forbidden_secret'\n",
			want: "mask=$((1<<n))\necho '" + sp("forbidden_secret") + "'\n",
		},
		// CHANGED EXPECTATION (was "backslash at end of a heredoc opener line
		// still flushes the body", want "cat <<EOF \\\n" + sp("body") +
		// "\nEOF\n").
		//
		// Ground truth, run against bash:
		//
		//	$ printf 'cat <<EOF \\\nbody\nEOF\n' > t.sh
		//	$ bash t.sh
		//	cat: body: No such file or directory
		//
		// bash joins the continued line into `cat <<EOF body` and passes `body`
		// to cat as a FILENAME ARGUMENT — so `body` is code, not heredoc data.
		// The body then begins on the next physical line, which is the `EOF`
		// delimiter, so the body is EMPTY and nothing on any line is blanked.
		//
		// Getting this wrong is the BLIND direction, not the loud one. This
		// projection keeps code and blanks data, and a forbidden-pattern rule
		// fires on what SURVIVES — so blanking `body` makes a marker written
		// there unmatchable and the rule reports a false PASS. Over-blanking on
		// destring-sh is silence, which is the failure class the fork's
		// unquoted-backslash work was filed against.
		"backslash-continued heredoc opener defers the body, so the joined line stays code": {
			in:   "cat <<EOF \\\nbody\nEOF\n",
			want: "cat <<EOF \\\nbody\nEOF\n",
		},
		// A6 (issue #6): a heredoc-looking token inside a '#' comment is NOT a
		// heredoc opener. The comment branch consumes the rest of the line
		// before the '<' branch can see it, so no body is queued and the file
		// tail survives. The validating port's scripts/lib/destring-sh.awk gets this
		// wrong and blanks every line to EOF hunting a terminator that never
		// arrives: check-gate-scripts-are-wired.sh:143 costs 378 lines,
		// check-flutter-paginated-cursor-must-be-consumed.sh:20 costs 328. These
		// pin the correctness that porting those gates to formwork depends on.
		"A6 repro: <<EOF inside a comment does not open a heredoc": {
			in:   "# `<<EOF … EOF` is not a command\necho 'forbidden_secret'\ntail\n",
			want: "# `<<EOF … EOF` is not a command\necho '" + sp("forbidden_secret") + "'\ntail\n",
		},
		"A6 repro: Future<<PaginatedMsg>> inside a comment does not open a heredoc": {
			in:   "#      `Future<<PaginatedMsg>>` in a signature\nreal code\n",
			want: "#      `Future<<PaginatedMsg>>` in a signature\nreal code\n",
		},
		// The same token in a trailing comment, where the '#' is mid-line
		// rather than at the start — the boundary rule must still reach it
		// before the '<' branch does.
		"A6 adversarial: <<EOF in a trailing comment does not open a heredoc": {
			in:   "x=1  # see <<EOF\necho \"forbidden_secret\"\n",
			want: "x=1  # see <<EOF\necho \"" + sp("forbidden_secret") + "\"\n",
		},
		// The discrimination that makes A6 more than a tautology: an opener
		// OUTSIDE a comment on the same shape still blanks its body. If the
		// comment handling ever grew into a blanket "ignore <<", the two
		// repros above would still pass and this one would not.
		"A6 discriminator: the same <<EOF outside a comment still blanks its body": {
			in:   "cat <<EOF\nforbidden_secret\nEOF\ntail\n",
			want: "cat <<EOF\n" + sp("forbidden_secret") + "\nEOF\ntail\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(DestringSh([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatal("line count changed")
			}
		})
	}
}

// TestDestringShBackslashInUnquotedText pins the ONE direction the lexer got
// wrong: a backslash escape was honoured inside double quotes but nowhere else,
// so a `\"` or `\'` sitting in UNQUOTED shell text — the ordinary shape of a
// bash `[[ =~ ]]` character class, e.g.
// `[[ "$1" =~ \.go([[:space:]\"\'\/]|$) ]]` — opened a phantom string that ran
// to the next quote or to EOF and blanked every comment after it. Any rule
// reading this projection then sees nothing where the comments were, which is
// silent blindness, not a false positive: the gate reports OK.
//
// Shell semantics: outside quotes, a backslash escapes the NEXT byte. So a
// `\"` is a literal quote character and can never open a string.
func TestDestringShBackslashInUnquotedText(t *testing.T) {
	cases := []destringShCase{
		{
			// The live shape, lifted from .claude/hooks/block-raw-code-search.sh.
			name: "escaped double quote in unquoted text opens nothing",
			src: `targets_go() {
  [[ "$1" =~ \.go([[:space:]\"\'\/]|$) ]] && return 0
  return 1
}
# hook notes that must remain visible to a comment-reading rule
`,
			keep: []string{"hook notes that must remain visible", "return 1"},
			gone: []string{"$1"},
		},
		{
			name: "escaped single quote in unquoted text opens nothing",
			src: `sep=\'
# a comment after a bare escaped apostrophe stays readable
echo "${sep}"
`,
			keep: []string{"a comment after a bare escaped apostrophe stays readable"},
			gone: []string{"${sep}"},
		},
		{
			name: "escaped hash in unquoted text is not a comment start",
			src: `printf '%s\n' \#not-a-comment
# but this one is a comment
`,
			keep: []string{"#not-a-comment", "but this one is a comment"},
			gone: []string{"%s"},
		},
		{
			// A backslash-newline is line joining, so the heredoc body begins
			// after the first UNESCAPED newline — verified against bash, which
			// reads `extra` as an argument and BODYSENTINEL as the body.
			name: "line continuation on a heredoc opener defers the body",
			src: `cat <<EOF \
  extra
BODYSENTINEL
EOF
# comment after the heredoc
`,
			keep: []string{"extra", "comment after the heredoc"},
			gone: []string{"BODYSENTINEL"},
		},

		// ---- regressions the escape must not break -------------------------
		{
			name: "double-quoted body still blanked, inner escaped quote still not a closer",
			src: `msg="say \" then still inside"
# comment after the string
`,
			keep: []string{"comment after the string"},
			gone: []string{"say", "then still inside"},
		},
		{
			name: "single-quoted body still blanked",
			src: `prog='inner literal text'
# comment after the awk program
`,
			keep: []string{"comment after the awk program"},
			gone: []string{"inner literal text"},
		},
		{
			name: "heredoc body still blanked",
			src: `cat <<'SQLTPL'
HEREDOCSENTINEL
SQLTPL
# comment after the heredoc
`,
			keep: []string{"comment after the heredoc"},
			gone: []string{"HEREDOCSENTINEL"},
		},
		{
			name: "hash glued to a word is parameter syntax, not a comment",
			src: `n=${#arr[@]}
b=${0##*/}
printf '%s\n' "$#"
`,
			keep: []string{"${#arr[@]}", "${0##*/}"},
			gone: []string{"%s"},
		},
		{
			name: "trailing backslash at end of input is safe",
			src:  `echo done \`,
			keep: []string{"echo done"},
		},
	}

	runDestringShCases(t, cases)
}

// TestDestringShBlankToEofDoors pins the two REMAINING doors through which the
// lexer blanks the rest of a file and goes silently blind — the same failure
// shape TestDestringShBackslashInUnquotedText closed for unquoted `\"`, in two
// constructs it does not reach.
//
//  1. ANSI-C quoting. Inside `$'...'` a backslash IS an escape (unlike a plain
//     `'...'`, where it is literal), so `$'don\'t'` is one string. The single
//     quote arm scans to the next `'` unconditionally, closes on the escaped
//     one, and the trailing `'` then opens a phantom string that runs to EOF.
//
//  2. Arithmetic left-shift by a NAMED variable. isHeredocWordStart accepts any
//     letter, so `$((1<<w))` parses `w` as a heredoc delimiter and the body
//     blanker eats every line to EOF looking for a `w` terminator that does not
//     exist. The word-start rule's claim that `$((1<<20))` is safe holds only
//     for numeric literals.
//
// Both are silent: a blanked comment produces no finding, so every rule reading
// this projection reports OK.
func TestDestringShBlankToEofDoors(t *testing.T) {
	cases := []destringShCase{
		{
			name: "ansi-c quoting: an escaped apostrophe does not close the string",
			src: `msg=$'ANSICSENTINEL\'tail'
# comment after an ansi-c string with an escaped apostrophe
echo "${msg}"
`,
			keep: []string{"comment after an ansi-c string with an escaped apostrophe"},
			gone: []string{"ANSICSENTINEL", "tail"},
		},
		{
			name: "ansi-c quoting: a plain body is still blanked",
			src: `nl=$'\n'
sep=$'PLAINANSIC'
# comment after a plain ansi-c string
`,
			keep: []string{"comment after a plain ansi-c string"},
			gone: []string{"PLAINANSIC"},
		},
		{
			name: "arithmetic left-shift by a named variable is not a heredoc opener",
			src: `n=$((1<<w))
# comment after a shift by a named variable
printf '%s\n' "${n}"
`,
			keep: []string{"comment after a shift by a named variable", "printf"},
			gone: []string{"%s"},
		},
		{
			name: "arithmetic left-shift with nested parens closes at the right ))",
			src: `n=$(( (a+b)<<shift ))
# comment after a nested-paren shift
`,
			keep: []string{"comment after a nested-paren shift"},
		},

		// ---- regressions the two fixes must not break ----------------------
		{
			name: "compound-assignment shift is still not a heredoc opener",
			src: `(( acc <<= step ))
# comment after a compound-assignment shift
`,
			keep: []string{"comment after a compound-assignment shift"},
		},
		{
			name: "numeric left-shift still not a heredoc opener",
			src: `n=$((1<<20))
# comment after a numeric shift
`,
			keep: []string{"comment after a numeric shift"},
		},
		{
			name: "a real heredoc after arithmetic still has its body blanked",
			src: `n=$((1<<w))
cat <<'EOF'
HEREDOCSENTINEL
EOF
# comment after the heredoc
`,
			keep: []string{"comment after the heredoc"},
			gone: []string{"HEREDOCSENTINEL"},
		},
		{
			name: "heredoc inside a command substitution still has its body blanked",
			src: `body="$(cat <<EOF
CMDSUBSENTINEL
EOF
)"
# comment after the command substitution
`,
			keep: []string{"comment after the command substitution"},
			gone: []string{"CMDSUBSENTINEL"},
		},
		{
			name: "a plain single-quoted string keeps its literal-backslash semantics",
			src: `prog='LITERALSENTINEL\'
# comment after a single-quoted string ending in a backslash
`,
			keep: []string{"comment after a single-quoted string ending in a backslash"},
			gone: []string{"LITERALSENTINEL"},
		},
		{
			name: "an escaped dollar leaves the following quote an ordinary string",
			src: `printf '%s\n' \$'ESCAPEDDOLLARSENTINEL'
# comment after an escaped dollar
`,
			keep: []string{"comment after an escaped dollar"},
			gone: []string{"ESCAPEDDOLLARSENTINEL"},
		},
	}

	runDestringShCases(t, cases)
}

// TestDestringShArithmeticCommandDoors pins the arithmetic forms the $((…))
// span tracker does not reach on its own.
//
// Only the arithmetic EXPANSION was tracked. bash has two more contexts with
// the same grammar, and in both a `<<` is a left shift:
//
//  1. the arithmetic COMMAND, `(( … ))` — the ordinary shape of a bash counter
//     or a C-style `for (( … ))` header;
//  2. `let`, every argument of which is an arithmetic expression.
//
// In both, `1 << n` parsed `n` as a heredoc delimiter word and the body blanker
// ate every line to EOF looking for a terminator that does not exist. Each rule
// declaring this preprocessor then silently stops scanning the affected file —
// a blanked comment produces no finding at all.
func TestDestringShArithmeticCommandDoors(t *testing.T) {
	cases := []destringShCase{
		{
			name: "arithmetic command: shift by a named variable is not a heredoc opener",
			src: `(( mask = 1 << n ))
# comment after an arithmetic command
printf '%s\n' "${mask}"
`,
			keep: []string{"comment after an arithmetic command", "printf"},
			gone: []string{"%s"},
		},
		{
			name: "c-style for header: shift by a named variable is not a heredoc opener",
			src: `for (( i = 0; i < 1 << w; i++ )); do :; done
# comment after a c-style for header
`,
			keep: []string{"comment after a c-style for header"},
		},
		{
			name: "let: shift by a named variable is not a heredoc opener",
			src: `let mask=1<<n
# comment after a let expression
`,
			keep: []string{"comment after a let expression"},
		},
		{
			// ADAPTED FROM THE FORK (was "unterminated heredoc blanks nothing",
			// keeping both lines).
			//
			// bash reads an unterminated heredoc to EOF — it warns
			// ("here-document delimited by end-of-file") and takes every
			// remaining line as the body. This projection does NOT follow it
			// there, deliberately.
			//
			// A file reaching this path is malformed shell, and the two
			// readings fail in opposite directions. Blanking to EOF is SILENT:
			// this projection keeps code and blanks data, so everything below
			// the stray opener stops matching and every rule reading it passes
			// a file it should fail — one unpaired `<<WORD` is enough to switch
			// a gate off for the rest of a file. Reading it back as ordinary
			// shell can only over-report, which is LOUD and gets corrected.
			// Fail-closed prefers loud, and the rest of this family already
			// does: an unterminated quote closes at the newline, not at EOF.
			name: "an unterminated heredoc is not a heredoc; its body stays readable",
			src: `cat <<NOPE
a body line whose terminator never arrives
# comment inside an unterminated heredoc
`,
			keep: []string{"cat <<NOPE", "a body line whose terminator never arrives", "comment inside an unterminated heredoc"},
		},

		// ---- regressions the three fixes must not break --------------------
		{
			name: "a let command ends at a semicolon, so a following heredoc still blanks",
			src: `let x=1<<n; cat <<'EOF'
LETHEREDOCSENTINEL
EOF
# comment after the heredoc
`,
			keep: []string{"comment after the heredoc"},
			gone: []string{"LETHEREDOCSENTINEL"},
		},
		{
			name: "a subshell after an arithmetic command still lexes normally",
			src: `(( n = 1 << w ))
( cat <<'EOF'
SUBSHELLSENTINEL
EOF
)
# comment after the subshell
`,
			keep: []string{"comment after the subshell"},
			gone: []string{"SUBSHELLSENTINEL"},
		},
		{
			name: "a name merely starting with let is not the let builtin",
			src: `letters="LETTERSSENTINEL"
# comment after a variable whose name starts with let
`,
			keep: []string{"comment after a variable whose name starts with let"},
			gone: []string{"LETTERSSENTINEL"},
		},
		{
			name: "nested arithmetic commands close at the right ))",
			src: `(( n = ((a + b)) << s ))
cat <<'EOF'
NESTEDARITHSENTINEL
EOF
# comment after nested arithmetic
`,
			keep: []string{"comment after nested arithmetic"},
			gone: []string{"NESTEDARITHSENTINEL"},
		},
		{
			// ADAPTED FROM THE FORK (was "a terminated heredoc is still blanked
			// when a later opener is not", keeping the trailing comment) — same
			// blanks-to-EOF resolution as the case above.
			// The terminated heredoc's body is data and is blanked; the later
			// unterminated one is not a heredoc at all, so its lines stay
			// readable. Pinned together because the interesting property is
			// that one stray opener does not disable the projection for
			// everything after it.
			name: "a terminated heredoc is blanked; a later unterminated one is not",
			src: `cat <<'REAL'
REALBODYSENTINEL
REAL
cat <<MISSING
# comment inside the unterminated second body
`,
			keep: []string{"cat <<MISSING", "comment inside the unterminated second body"},
			gone: []string{"REALBODYSENTINEL"},
		},
	}

	runDestringShCases(t, cases)
}

// destringShCase is the keep/gone case shape the three tables above share: it
// asserts on the PROJECTION (which substrings survive) rather than on exact
// bytes, which is the readable way to pin a multi-line lexer trace.
type destringShCase struct {
	name string
	src  string
	keep []string
	gone []string
}

func runDestringShCases(t *testing.T, cases []destringShCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := string(DestringSh([]byte(tc.src)))
			if got, want := strings.Count(out, "\n"), strings.Count(tc.src, "\n"); got != want {
				t.Fatalf("line structure changed: %d newlines, want %d", got, want)
			}
			if len(out) != len(tc.src) {
				t.Fatalf("length changed: %d bytes, want %d", len(out), len(tc.src))
			}
			for _, s := range tc.keep {
				if !strings.Contains(out, s) {
					t.Errorf("projection lost %q\n--- got ---\n%s", s, out)
				}
			}
			for _, s := range tc.gone {
				if strings.Contains(out, s) {
					t.Errorf("projection kept %q, which is string/heredoc data\n--- got ---\n%s", s, out)
				}
			}
		})
	}
}
