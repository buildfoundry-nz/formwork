package declspan

import "testing"

func TestGoExtentEndsAtTheDeclarationItOpens(t *testing.T) {
	src := []byte(`package p

import "fmt"

// Owner is the one place that may say it.
func Owner(x int) string {
	s := "a { brace in a string"
	r := ` + "`raw { too\n}`" + `
	if x > 0 {
		return s + r
	}
	return fmt.Sprint(x)
}

func (r *R) Method() {}

const ownerSQL = ` + "`" + `
SELECT 1;
` + "`" + `

var table = []struct {
	a int
}{
	{a: 1},
}

func Sibling() {}
`)
	for _, tc := range []struct {
		name string
		line int
		want Span
	}{
		{"func with a brace in a string and a raw string", 6, Span{6, 14}},
		{"method on one line", 16, Span{16, 16}},
		{"const with a raw-string value", 18, Span{18, 20}},
		{"var with a composite literal", 22, Span{22, 26}},
		{"package clause is the whole file", 1, Span{1, 28}},
		{"a blank line declares only itself", 2, Span{2, 2}},
		{"a comment line declares only itself", 5, Span{5, 5}},
	} {
		if got := Extent("a.go", src, tc.line); got != tc.want {
			t.Errorf("%s: Extent(line %d) = %+v, want %+v", tc.name, tc.line, got, tc.want)
		}
	}
}

func TestDartExtentIgnoresDelimitersInStringsAndComments(t *testing.T) {
	src := []byte(`import 'x.dart';

class Owner extends StatelessWidget {
  // a } in a comment
  final s = '${label(x)} }';
  Widget build(BuildContext c) {
    return Text(s);
  }
}

int arrow(int x) => list.map((y) {
  return y;
}).length;

class Sibling {}
`)
	for _, tc := range []struct {
		name string
		line int
		want Span
	}{
		{"class with braces in a comment and an interpolation", 3, Span{3, 9}},
		{"arrow function ends at its depth-zero semicolon", 11, Span{11, 13}},
		{"one-line class", 15, Span{15, 15}},
	} {
		if got := Extent("w.dart", src, tc.line); got != tc.want {
			t.Errorf("%s: Extent(line %d) = %+v, want %+v", tc.name, tc.line, got, tc.want)
		}
	}
}

func TestProtoExtentIsTheMessage(t *testing.T) {
	src := []byte("syntax = \"proto3\";\n\nmessage Owner {\n  // }\n  string a = 1; // \"}\"\n}\n\nmessage Other {}\n")
	if got := Extent("x.proto", src, 3); got != (Span{3, 6}) {
		t.Fatalf("Extent = %+v, want {3 6}", got)
	}
}

func TestADocumentDeclaresItself(t *testing.T) {
	src := []byte("> **Historical record.**\n\nbody\nmore\n")
	if got := Extent("docs/a.md", src, 1); got != (Span{1, 4}) {
		t.Fatalf("Extent = %+v, want the whole document {1 4}", got)
	}
}

func TestMaskBlanksOnlyTheSpanAndKeepsLines(t *testing.T) {
	src := []byte("a\nbb\nccc\nd")
	got := string(Mask(src, []Span{{2, 3}}))
	if got != "a\n  \n   \nd" {
		t.Fatalf("Mask = %q", got)
	}
}

func TestDirectivesAreTheFilesImports(t *testing.T) {
	goSrc := []byte("package p\n\nimport \"fmt\"\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\nfunc F() {}\n")
	got := Directives("a.go", goSrc)
	if len(got) != 2 || got[0] != (Span{3, 3}) || got[1] != (Span{5, 8}) {
		t.Fatalf("Go directives = %+v, want {3 3} and {5 8}", got)
	}
	dartSrc := []byte("library x;\n\nimport 'package:dio/dio.dart';\nexport 'a.dart'\n    show A;\n\nclass C {\n  // import 'not.dart';\n}\n")
	got = Directives("w.dart", dartSrc)
	if len(got) != 3 || got[0] != (Span{1, 1}) || got[1] != (Span{3, 3}) || got[2] != (Span{4, 5}) {
		t.Fatalf("Dart directives = %+v, want {1 1} {3 3} {4 5}", got)
	}
	if got := Directives("x.sql", []byte("import x;\n")); got != nil {
		t.Fatalf("a non-Go, non-Dart file has no directives, got %+v", got)
	}
}
