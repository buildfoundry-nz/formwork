package preprocess

import (
	"strings"
	"testing"
)

// TestDestringDecommentSh covers the combined transform: everything
// destring-sh blanks (string interiors, heredoc bodies), plus the bodies of
// '#' comments. The comment blanking is what destring-sh deliberately does
// NOT do — it only skips over comments so an apostrophe inside one cannot
// open a phantom string — and it is the reason the validating port's two
// shell-meta gates pipe destring-sh.awk through a sed comment-stripper before
// matching.
func TestDestringDecommentSh(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"full-line comment blanked including the hash": {
			in:   "# a comment\necho hi\n",
			want: sp("# a comment") + "\necho hi\n",
		},
		"trailing comment blanked, code before it kept": {
			in:   "echo hi  # trailing\n",
			want: "echo hi  " + sp("# trailing") + "\n",
		},
		"comment with no trailing newline blanked to EOF": {
			in:   "echo hi # end",
			want: "echo hi " + sp("# end"),
		},
		// The de-stringing half is unchanged: string interiors still go, and
		// the quotes themselves still survive.
		"string interiors still blanked, quotes kept": {
			in:   "echo 'secret pw'\n",
			want: "echo '" + sp("secret pw") + "'\n",
		},
		"heredoc bodies still blanked, delimiters kept": {
			in:   "cat <<EOF\nsecret line\nEOF\n",
			want: "cat <<EOF\n" + sp("secret line") + "\nEOF\n",
		},
		// A '#' glued to the preceding word is not a comment start, so
		// parameter expansions survive intact — the same boundary rule
		// destring-sh uses, and the property the gates' sed regex was
		// hand-written to preserve.
		"parameter expansion ${x#foo} is not a comment": {
			in:   "y=${x#foo}\n",
			want: "y=${x#foo}\n",
		},
		"$# is not a comment": {
			in:   "n=$# echo hi\n",
			want: "n=$# echo hi\n",
		},
		// Review finding: blanking must use a NARROWER boundary than skipping.
		// The reference pipeline strips comments with
		// `sed -E 's/(^|[[:space:]])#.*$//'` — start-of-line, space or tab only.
		// destring-sh additionally treats ';', '|', '&', '(', ')', '<', '>' as
		// boundaries so an apostrophe in `cmd;#don't` cannot open a phantom
		// string, but blanking on that wider set DELETES code the sed keeps,
		// which is the missed-violation direction. So: skip on the wide
		// boundary, blank only on the narrow one.
		"# after a word-breaking metacharacter is skipped but NOT blanked": {
			in:   "echo hi;#c it's\necho 'x'\n",
			want: "echo hi;#c it's\necho '" + sp("x") + "'\n",
		},
		// The concrete miss: in bash `x=$(ls)#tag` is one word, so the '#' is
		// not a comment at all. Blanking on the ')' boundary erased the rest of
		// the line — including a real violation on it.
		"code after $(cmd)#tag survives, violation on the line stays visible": {
			in:   "x=$(ls)#tag; find . | grep -q needle\n",
			want: "x=$(ls)#tag; find . | grep -q needle\n",
		},
		// A '#' inside a quoted string is string content, not a comment
		// start: the string branch reaches it first and blanks it as
		// interior. The distinction matters because the quotes must survive
		// (a comment would have eaten the closing quote and everything after
		// it on the line).
		"# inside a string is string content, not a comment start": {
			in:   "grep -n '#' file\necho done\n",
			want: "grep -n '" + sp("#") + "' file\necho done\n",
		},
		// Issue #6, inherited from destring-sh: a heredoc-looking token in a
		// comment must not open a heredoc. Here the comment is blanked as
		// well, so the token is gone twice over — but the file tail is what
		// matters, and it survives.
		"<<EOF inside a comment neither opens a heredoc nor survives": {
			in:   "# `<<EOF … EOF` is not a command\necho 'forbidden_secret'\ntail\n",
			want: sp("# `<<EOF … EOF` is not a command") + "\necho '" + sp("forbidden_secret") + "'\ntail\n",
		},
		// The motivating case for the whole transform. In the validating port
		// every gate header quotes the shape its own rule bans; matching against
		// destring-sh output alone would report the header comment as a
		// violation. After comment blanking only the real code line matches.
		"gate header quoting the banned shape is blanked; the real call site is not": {
			in:   "# never write `producer | grep -q PAT`\nfind . | grep -q PAT\n",
			want: sp("# never write `producer | grep -q PAT`") + "\nfind . | grep -q PAT\n",
		},
		"empty input": {in: "", want: ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(DestringDecommentSh([]byte(c.in)))
			if got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if strings.Count(got, "\n") != strings.Count(c.in, "\n") {
				t.Fatal("line count changed")
			}
		})
	}
}

func TestDestringDecommentShRegistered(t *testing.T) {
	if _, ok := Lookup("destring-decomment-sh"); !ok {
		t.Fatal("destring-decomment-sh is not registered")
	}
}
