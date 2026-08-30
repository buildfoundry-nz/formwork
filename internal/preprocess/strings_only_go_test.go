package preprocess

import (
	"strings"
	"testing"
)

func TestStringsOnlyGo(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"keeps interpreted string interior, blanks the rest": {
			in:   `s := "keep" // c` + "\n",
			want: sp(`s := "`) + "keep" + sp(`" // c`) + "\n",
		},
		"keeps raw string interior across lines": {
			in:   "x := `a\nb`\n",
			want: sp("x := `") + "a\nb" + sp("`") + "\n",
		},
		"string inside comment is not restored": {
			in:   `// "c"` + "\n",
			want: sp(`// "c"`) + "\n",
		},
		"rune interior is not kept": {
			in:   "r := 'x'\n",
			want: sp("r := 'x'") + "\n",
		},
		"two strings on one line": {
			in:   `a("one", "two")` + "\n",
			want: sp(`a("`) + "one" + sp(`", "`) + "two" + sp(`")`) + "\n",
		},
		"escaped quote stays inside": {
			in:   `s := "a\"b"` + "\n",
			want: sp(`s := "`) + `a\"b` + sp(`"`) + "\n",
		},
		"empty input": {in: "", want: ""},
		// A6: same repro as decomment_go_test.go, other direction. The
		// unterminated string's interior must stop at the newline (kept:
		// "abc\"), and the *next* line's properly quoted "next" string must
		// still be recognized as a string (interior kept) rather than
		// getting corrupted into bogus code/string regions that blank
		// "next" or swallow the comment into a fake string.
		"A6: unterminated string ending in backslash does not swallow the newline (next line's string and comment unaffected)": {
			in:   "s := \"abc\\\nd := \"next\" // comment\n",
			want: sp(`s := "`) + `abc\` + "\n" + sp(`d := "`) + "next" + sp(`" // comment`) + "\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(StringsOnlyGo([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatal("line count changed")
			}
		})
	}
}
