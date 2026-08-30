package preprocess

import (
	"strings"
	"testing"
)

// sp returns spaces of the same length as s — the blanked form of removed
// text. Tests build expected output with it instead of hand-counting spaces.
func sp(s string) string { return strings.Repeat(" ", len(s)) }

func TestDecommentGo(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"line comment blanked": {
			in:   "code() // trailing comment\n",
			want: "code() " + sp("// trailing comment") + "\n",
		},
		"comment marker inside string kept": {
			in:   `s := "http://x" // real` + "\n",
			want: `s := "http://x" ` + sp("// real") + "\n",
		},
		"comment marker inside raw string kept": {
			in:   "s := `// not a comment`\n",
			want: "s := `// not a comment`\n",
		},
		"slash inside rune kept": {
			in:   "r := '/'\n",
			want: "r := '/'\n",
		},
		"block comment single line": {
			in:   "a /* mid */ b\n",
			want: "a " + sp("/* mid */") + " b\n",
		},
		"block comment multi line preserves newlines": {
			in:   "a /* one\ntwo */ b\n",
			want: "a " + sp("/* one") + "\n" + sp("two */") + " b\n",
		},
		"unterminated block comment runs to EOF": {
			in:   "a /* never closed\nmore\n",
			want: "a " + sp("/* never closed") + "\n" + sp("more") + "\n",
		},
		"escaped quote does not end string": {
			in:   `s := "a\"b // x" // c` + "\n",
			want: `s := "a\"b // x" ` + sp("// c") + "\n",
		},
		"comment between strings": {
			in:   `a := "x" /* c */ + "y"` + "\n",
			want: `a := "x" ` + sp("/* c */") + ` + "y"` + "\n",
		},
		"empty input": {in: "", want: ""},
		// A6: an unterminated interpreted string ending in a backslash must
		// not skip the escape when the next byte is the newline itself —
		// otherwise the string swallows the newline and runs across lines,
		// misclassifying the next line's real string terminator and
		// comment. Here the corruption would leave "// comment" unblanked
		// (misclassified as part of a bogus string/code region).
		"A6: unterminated string ending in backslash does not swallow the newline (comment still blanked)": {
			in:   "s := \"abc\\\nd := \"next\" // comment\n",
			want: "s := \"abc\\\nd := \"next\" " + sp("// comment") + "\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(DecommentGo([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatalf("line count changed: %d -> %d", strings.Count(c.in, "\n"), strings.Count(got, "\n"))
			}
		})
	}
}

func TestDecommentGoDoesNotMutateInput(t *testing.T) {
	in := []byte("code() // c\n")
	orig := string(in)
	_ = DecommentGo(in)
	if string(in) != orig {
		t.Fatal("input mutated")
	}
}
