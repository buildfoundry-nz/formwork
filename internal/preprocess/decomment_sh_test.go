package preprocess

import (
	"strings"
	"testing"
)

// TestDecommentSh covers the comments-only projection: blankable comments go,
// string interiors and heredoc bodies STAY — the exact inverse of
// destring-sh on data spans, for gates whose bash source stripped comment
// lines but matched against string contents (the buf-breaking baseline gate:
// `branch=staging` lives inside a quoted --against ref).
func TestDecommentSh(t *testing.T) {
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
		// The whole point: data spans survive untouched.
		"string interiors kept": {
			in:   "echo 'branch=staging'\n",
			want: "echo 'branch=staging'\n",
		},
		"double-quoted ref kept": {
			in:   "buf breaking --against \".git#branch=staging,subdir=s\"\n",
			want: "buf breaking --against \".git#branch=staging,subdir=s\"\n",
		},
		"heredoc body kept": {
			in:   "cat <<EOF\nmerge-base\nEOF\n",
			want: "cat <<EOF\nmerge-base\nEOF\n",
		},
		// Same boundary rules as the rest of the family.
		"parameter expansion ${x#foo} is not a comment": {
			in:   "y=${x#foo}\n",
			want: "y=${x#foo}\n",
		},
		"# after a word-breaking metacharacter is skipped but NOT blanked": {
			in:   "echo hi;#c it's\necho 'x'\n",
			want: "echo hi;#c it's\necho 'x'\n",
		},
		"apostrophe inside a blanked comment opens no phantom string": {
			in:   "# don't\nmerge-base\n",
			want: sp("# don't") + "\nmerge-base\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(DecommentSh([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
			if strings.Count(got, "\n") != strings.Count(tc.in, "\n") {
				t.Fatalf("line structure changed: got %q", got)
			}
		})
	}
}
