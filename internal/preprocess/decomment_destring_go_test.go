package preprocess

import "testing"

// DecommentDestringGo exists because decomment-go alone leaves a
// required-pattern satisfiable by a string literal. A rule that requires
// `\.CanCreate\(` to prove the pure decision seam is still called passes on
//
//	slog.InfoContext(ctx, "quota denied on the plan.CanCreate() fast path")
//
// with the real call deleted. That is not hypothetical for the entitlements
// package: it already carries several fmt.Errorf("entitlements.ConsumeTakeoff:
// %w", err) strings, so deleting the call while leaving an error-wrap string
// behind is the natural shape of a refactor, not a contrivance.
//
// So this transform blanks comments AND the interiors of string/rune literals,
// leaving only real code for the matcher.
func TestDecommentDestringGo(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "line comment is blanked",
			src:  "x := 1 // plan.CanCreate()\n",
			want: "x := 1                    \n",
		},
		{
			name: "string interior is blanked but the quotes survive",
			src:  "s := \"plan.CanCreate()\"\n",
			want: "s := \"                \"\n",
		},
		{
			name: "raw string interior is blanked",
			src:  "s := `plan.CanCreate()`\n",
			want: "s := `                `\n",
		},
		{
			name: "rune literal interior is blanked",
			src:  "r := 'x'\n",
			want: "r := ' '\n",
		},
		{
			name: "real code survives untouched",
			src:  "if !plan.CanCreate() {\n",
			want: "if !plan.CanCreate() {\n",
		},
		{
			name: "block comment spanning lines keeps its line structure",
			src:  "a\n/* plan.CanCreate()\n   more */\nb\n",
			want: "a\n                   \n          \nb\n",
		},
		{
			name: "a comment marker inside a string is not treated as a comment",
			src:  "s := \"// not a comment\"\nreal()\n",
			want: "s := \"                \"\nreal()\n",
		},
		{
			name: "a quote inside a comment does not open a string",
			src:  "// he said \"hi\n x := 1\n",
			want: "              \n x := 1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(DecommentDestringGo([]byte(tc.src)))
			if got != tc.want {
				t.Fatalf("DecommentDestringGo(%q)\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

// Line-preservation is a hard requirement of the Transform contract —
// scan.File.Variant errors if the newline count changes.
func TestDecommentDestringGoPreservesLineCount(t *testing.T) {
	src := []byte("package p\n// c\nfunc f() {\n\ts := `a\nb`\n\t_ = 'x'\n}\n")
	got := DecommentDestringGo(src)
	if len(got) != len(src) {
		t.Fatalf("length changed: got %d, want %d", len(got), len(src))
	}
	count := func(b []byte) int {
		n := 0
		for _, c := range b {
			if c == '\n' {
				n++
			}
		}
		return n
	}
	if count(got) != count(src) {
		t.Fatalf("newline count changed: got %d, want %d", count(got), count(src))
	}
}
