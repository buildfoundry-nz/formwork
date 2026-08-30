package preprocess

import (
	"strings"
	"testing"
)

// The awk member of the comments-only family. awk needs its own lexer rather
// than a scope widening of the shell one, because the two grammars differ
// exactly where it matters for locating a '#':
//
//   - awk has /regex/ literals, and a '#' inside one is regex content, not a
//     comment opener. Deciding whether a '/' opens a regex or is division
//     needs token-context tracking — a regex literal is only valid where an
//     operand is expected.
//   - awk has no heredocs and no single-quoted strings. A shell lexer treats
//     an apostrophe as opening a literal that spans lines, so it desyncs on
//     the first apostrophe in awk code or prose.
//
// The regex-vs-division cases below are the ones a naive lexer gets wrong.
func TestCommentsOnlyAwkRegisters(t *testing.T) {
	if _, ok := Lookup("comments-only-awk"); !ok {
		t.Fatalf("preprocess %q is not registered; a rule declaring it cannot load. registered: %v",
			"comments-only-awk", Names())
	}
}

func TestCommentsOnlyAwk(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"comment body survives, code is blanked": {
			in:   "BEGIN { n = 0 } # counts lines\n",
			want: sp("BEGIN { n = 0 } #") + " counts lines\n",
		},

		// DIVISION. The '/' follows a value, so it is an operator and the '#'
		// later on the line really does open a comment.
		"division after an identifier leaves the later # a comment": {
			in:   "n = length / 2 # halve\n",
			want: sp("n = length / 2 #") + " halve\n",
		},
		"division after a closing paren": {
			in:   "n = (a + b) / 2 # mean\n",
			want: sp("n = (a + b) / 2 #") + " mean\n",
		},
		"division after a subscript": {
			in:   "n = a[i] / 2 # scale\n",
			want: sp("n = a[i] / 2 #") + " scale\n",
		},
		"division after a number": {
			in:   "n = 10 / 2 # half\n",
			want: sp("n = 10 / 2 #") + " half\n",
		},

		// REGEX. The '/' is in operand position, so the '#' inside it is
		// regex content and must NOT be read as a comment opener.
		"hash inside a regex literal is not a comment": {
			in:   "$0 ~ /a#b/ { print }\n",
			want: sp("$0 ~ /a#b/ { print }") + "\n",
		},
		"regex as a bare pattern at the start of a rule": {
			in:   "/^#include/ { print }\n",
			want: sp("/^#include/ { print }") + "\n",
		},
		"regex in an argument position after a comma": {
			in:   "split(s, arr, /#/) # sep\n",
			want: sp("split(s, arr, /#/) #") + " sep\n",
		},
		// The classic ambiguity: ')' normally ends a value, which would make
		// the next '/' division. After an if/while/for CONDITION it does not —
		// a statement follows, so a '/' there opens a regex.
		"regex directly after an if condition, not division": {
			in:   "if (x) /re#gex/\n",
			want: sp("if (x) /re#gex/") + "\n",
		},
		"regex after a statement keyword": {
			in:   "print / 2 # not division\n",
			want: sp("print / 2 # not division") + "\n",
		},
		// A '/' inside a bracket expression is literal and does not close the
		// regex, so the regex runs to the '/' after the '#'.
		"slash inside a regex bracket expression does not close it": {
			in:   "$0 ~ /[/]#/ { print }\n",
			want: sp("$0 ~ /[/]#/ { print }") + "\n",
		},
		"escaped slash does not close the regex": {
			in:   "$0 ~ /a\\/b#c/ { print }\n",
			want: sp("$0 ~ /a\\/b#c/ { print }") + "\n",
		},

		// STRINGS. awk has double-quoted strings only.
		"hash inside a string is not a comment": {
			in:   "print \"a#b\" # real\n",
			want: sp("print \"a#b\" #") + " real\n",
		},
		"escaped quote keeps the string open": {
			in:   "print \"a\\\"#b\" # real\n",
			want: sp("print \"a\\\"#b\" #") + " real\n",
		},
		// awk has NO single-quoted strings. An apostrophe is ordinary text; a
		// shell lexer would open a literal here and swallow the rest of the
		// file, blanking every comment after it.
		"apostrophe does not open a string": {
			in:   "# don't stop\nprint \"x\" # kept\n",
			want: sp("#") + " don't stop\n" + sp("print \"x\" #") + " kept\n",
		},

		// STRUCTURE.
		"multiple comment lines each keep their body": {
			in:   "# one\nx = 1\n# two\n",
			want: sp("#") + " one\n" + sp("x = 1") + "\n" + sp("#") + " two\n",
		},
		"a comment opener inside a comment is just text": {
			in:   "# a # b\n",
			want: sp("#") + " a # b\n",
		},
		"empty input": {in: "", want: ""},
		"no trailing newline": {
			in:   "x = 1 # end",
			want: sp("x = 1 #") + " end",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(CommentsOnlyAwk([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatal("line count changed")
			}
			if len(got) != len(c.in) {
				t.Fatalf("length changed: %d, want %d", len(got), len(c.in))
			}
		})
	}
}

// The transform must be pure: no lexer state may survive a call, or the
// projection of one file would depend on whichever file was scanned before it.
func TestCommentsOnlyAwkResetsStatePerCall(t *testing.T) {
	// An unterminated regex leaves the lexer mid-literal at EOF.
	unterminated := "$0 ~ /never closed\n"
	if _, ok := Lookup("comments-only-awk"); !ok {
		t.Fatal("comments-only-awk is not registered")
	}
	CommentsOnlyAwk([]byte(unterminated))

	src := "# marker\n"
	want := sp("#") + " marker\n"
	for i := range 3 {
		if got := string(CommentsOnlyAwk([]byte(src))); got != want {
			t.Fatalf("call %d: got %q, want %q", i, got, want)
		}
	}
}

// An unterminated literal costs one line of projection, not the rest of the
// file. Blanking to EOF is the silent direction: a marker below it would be
// unmatchable, so the gate would pass a file it should fail.
func TestCommentsOnlyAwkBoundsUnterminatedLiterals(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"unterminated regex stops at the newline": {
			in:   "$0 ~ /never closed\n# still a comment\n",
			want: sp("$0 ~ /never closed") + "\n" + sp("#") + " still a comment\n",
		},
		"unterminated string stops at the newline": {
			in:   "print \"never closed\n# still a comment\n",
			want: sp("print \"never closed") + "\n" + sp("#") + " still a comment\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := string(CommentsOnlyAwk([]byte(c.in))); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}
