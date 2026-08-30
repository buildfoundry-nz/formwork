package preprocess

import (
	"strings"
	"testing"
)

// code-only-dart is the projection a DELIMITER-COUNTING rule needs. A rule that
// walks an argument list by bracket depth over raw Dart loses the end of the
// invocation the moment a string literal carries a parenthesis — and Dart
// string interpolation puts arbitrary code, brackets and all, inside literals
// (`'${label(x)}'`). Under this projection a literal contributes no delimiters
// at all, only its quotes.
//
// The contract: comment bytes and string CONTENTS become spaces, the quote
// delimiters and everything else survive verbatim, and line structure is
// preserved so projection line N joins back to source line N.
//
// It is the exact complement of CommentsOnlyDart and shares that lexer, so the
// two are tested against the same shapes.

func TestCodeOnlyDartIsRegistered(t *testing.T) {
	if _, ok := Lookup("code-only-dart"); !ok {
		t.Fatalf("preprocess %q is not registered; a rule declaring it cannot load. registered: %v",
			"code-only-dart", Names())
	}
}

func TestCodeOnlyDart(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "line comment is blanked, code around it survives",
			src:  "final x = 1; // keyboardType: TextInputType.number\nfinal y = 2;",
			want: "final x = 1;                                      \nfinal y = 2;",
		},
		{
			name: "dartdoc is blanked",
			src:  "/// validator: null\nfinal x = 1;",
			want: "                   \nfinal x = 1;",
		},
		{
			name: "nesting block comment is blanked to its true end",
			src:  "a(/* outer /* inner */ still comment */ b);",
			want: "a(                                      b);",
		},
		{
			name: "string CONTENTS go, the quotes stay",
			src:  "Text('validator: (_) => null');",
			want: "Text('                      ');",
		},
		{
			name: "interpolation contributes no brackets",
			src:  "Text('${label(dense ? a : b)}');",
			want: "Text('                       ');",
		},
		{
			name: "raw string keeps its r and quotes",
			src:  `RegExp(r"^[0-9]+$");`,
			want: `RegExp(r"        ");`,
		},
		{
			name: "escaped quote does not end the literal early",
			src:  `x = 'it\'s (here)'; y = 1;`,
			want: `x = '            '; y = 1;`,
		},
		{
			name: "an unbalanced quote costs one line, not the rest of the file",
			src:  "x = 'oops;\nfinal y = (1);",
			want: "x = '     \nfinal y = (1);",
		},
		{
			name: "triple-quoted string spans lines and keeps them",
			src:  "x = '''a(\nb)''';\ny = 2;",
			want: "x = '''  \n  ''';\ny = 2;",
		},
		{
			name: "a comment token inside a string is data, not a comment",
			src:  "x = '// not a comment'; final y = 2;",
			want: "x = '                '; final y = 2;",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := project(t, "code-only-dart", tc.src)
			if got != tc.want {
				t.Errorf("code-only-dart mismatch\n src: %q\n got: %q\nwant: %q", tc.src, got, tc.want)
			}
			if a, b := strings.Count(tc.src, "\n"), strings.Count(got, "\n"); a != b {
				t.Errorf("not line-preserving: input has %d newline(s), output has %d", a, b)
			}
		})
	}
}

// The two halves of the same lexer must partition the source: every byte is in
// exactly one of them, so a byte cannot be both code and comment-content, and
// none can be lost by both.
func TestCodeOnlyDartComplementsCommentsOnlyDart(t *testing.T) {
	src := "// lead\nfinal a = 'text(1)'; /* mid */ b(c);\n"
	code := project(t, "code-only-dart", src)
	comments := project(t, "comments-only-dart", src)
	if len(code) != len(src) || len(comments) != len(src) {
		t.Fatalf("projections must preserve length: src=%d code=%d comments=%d",
			len(src), len(code), len(comments))
	}
	for i := range src {
		if src[i] == '\n' {
			continue
		}
		codeKept, commentKept := code[i] == src[i], comments[i] == src[i]
		if codeKept && commentKept && src[i] != ' ' {
			t.Errorf("byte %d (%q) survives BOTH projections", i, src[i])
		}
	}
}
