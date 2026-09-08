package preprocess

import "testing"

func TestDecommentDart(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"line", "call(); //gone\n", "call();       \n"},
		{"block", "a/*gone*/b", "a        b"},
		{"nested-block", "a/*a/*b*/c*/b", "a           b"},
		{"single", "'//literal' //gone\n", "'//literal'       \n"},
		{"double", "\"/*literal*/\"/*gone*/", "\"/*literal*/\"        "},
		{"raw-single", "r'\\'//gone\n", "r'\\'      \n"},
		{"raw-double", "r\"\\\"//gone\n", "r\"\\\"      \n"},
		{"triple-single", "'''a\n//literal\nb'''//gone\n", "'''a\n//literal\nb'''      \n"},
		{"triple-double", "\"\"\"a\n/*literal*/\nb\"\"\"//gone\n", "\"\"\"a\n/*literal*/\nb\"\"\"      \n"},
		{"raw-triple-single", "r'''\\'''//gone\n", "r'''\\'''      \n"},
		{"raw-triple-double", "r\"\"\"\\\"\"\"//gone\n", "r\"\"\"\\\"\"\"      \n"},
		{"escaped-quote", "'\\'//literal'//gone\n", "'\\'//literal'      \n"},
		{"escaped-dollar", "'\\${x //literal}'//gone\n", "'\\${x //literal}'      \n"},
		{"simple-interpolation", "'hello $name //literal'//gone\n", "'hello $name //literal'      \n"},
		{"quoted-comment-in-interpolation", "'${'//'.length}${pan(Offset(0, e.scrollDelta.dy))}'//gone\n", "'${'//'.length}${pan(Offset(0, e.scrollDelta.dy))}'      \n"},
		{"nested-interpolation", "'${f('${'//'}')}'//gone\n", "'${f('${'//'}')}'      \n"},
		{"comment-in-interpolation", "'${(/*gone*/ 3)}'//gone\n", "'${(         3)}'      \n"},
		{"braces-in-interpolation", "'${{'x': {/*gone*/}}}'//gone\n", "'${{'x': {        }}}'      \n"},
		{"multiline-interpolation", "'${f(//gone\n'//literal')}'//gone\n", "'${f(      \n'//literal')}'      \n"},
		{"raw-interpolation-text", "r'${/*literal*/}'//gone\n", "r'${/*literal*/}'      \n"},
		{"unterminated-string-line-bound", "'oops\ncall(); //gone\n", "'oops\ncall();       \n"},
		{"unterminated-block", "call(); /*gone\nmore", "call();       \n    "},
		{"crlf", "//gone\r\ncall();", "       \ncall();"},
		{"unicode", "é//é\n", "é    \n"},
		{"comment-after-nested-brace", "'${{'x': {}} /*gone*/}'//gone\n", "'${{'x': {}}         }'      \n"},
		{"partial-triple-close", "'''a''//literal\nb'''//gone\n", "'''a''//literal\nb'''      \n"},
		{"empty-literal", "''//gone\n", "''      \n"},
		{"empty", "", ""},
		{"trailing-escape", "'x\\", "'x\\"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := project(t, "decomment-dart", tc.src)
			if got != tc.want {
				t.Fatalf("projection = %q; want %q", got, tc.want)
			}
			if len(got) != len(tc.src) {
				t.Fatalf("byte positions changed: got %d bytes, want %d", len(got), len(tc.src))
			}
		})
	}
}

func TestDecommentDartOwnsOutput(t *testing.T) {
	transform, ok := Lookup("decomment-dart")
	if !ok || transform == nil {
		t.Fatal("decomment-dart is not registered")
	}
	for _, source := range []string{"//gone\nkeep", "keep"} {
		input := []byte(source)
		output := transform(input)
		if string(input) != source {
			t.Fatal("projection mutated its input")
		}
		if len(output) == 0 {
			t.Fatal("projection discarded its output")
		}
		output[0] = 'X'
		if string(input) != source {
			t.Fatal("output aliases the shared input")
		}
	}
}
