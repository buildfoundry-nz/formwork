package preprocess

import (
	"strings"
	"testing"
)

// The comments-only family is the projection an annotation-marker rule needs:
// it answers "is this token inside a COMMENT — regardless of where in the
// comment it sits?" A comment-opener regex cannot answer that. It (a) misses a
// marker on a bare continuation line of a multi-line block comment (that line
// carries no comment token at all), and (b) accuses comment-shaped bytes that
// live INSIDE a string literal, where they are data. Only comment/string state
// tracked byte-by-byte across lines gets both directions right.
//
// The contract these tests state: comment CONTENTS survive verbatim,
// delimiters and everything else become spaces, and line structure is
// preserved so projection line N joins back to source line N.
//
// TestCommentsOnlyGo's source below is reused verbatim by the validating
// port, which still runs a second awk implementation of the Go
// member on its own path, so the two answer to one table. Keep the sources
// here literal and self-contained for that reason.

func TestCommentsOnlyTransformsAreRegistered(t *testing.T) {
	for _, name := range []string{"comments-only-go", "comments-only-dart", "comments-only-sql"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("preprocess %q is not registered; a rule declaring it cannot load. registered: %v",
				name, Names())
		}
	}
}

// project resolves a transform through the registry — the same path a rule
// takes — so a transform that exists but is never registered still fails.
func project(t *testing.T, name, src string) string {
	t.Helper()
	tr, ok := Lookup(name)
	if !ok || tr == nil {
		t.Fatalf("preprocess %q is not registered", name)
	}
	out := string(tr([]byte(src)))
	if got, want := strings.Count(out, "\n"), strings.Count(src, "\n"); got != want {
		t.Fatalf("%s: line structure changed: %d newlines, want %d", name, got, want)
	}
	if len(out) != len(src) {
		t.Fatalf("%s: length changed: %d bytes, want %d", name, len(out), len(src))
	}
	return out
}

func assertProjection(t *testing.T, name, src string, keep, gone []string) {
	t.Helper()
	out := project(t, name, src)
	for _, s := range keep {
		if !strings.Contains(out, s) {
			t.Errorf("%s: comment text %q was dropped\n--- got ---\n%s", name, s, out)
		}
	}
	for _, s := range gone {
		if strings.Contains(out, s) {
			t.Errorf("%s: non-comment text %q survived\n--- got ---\n%s", name, s, out)
		}
	}
}

func TestCommentsOnlyGo(t *testing.T) {
	src := "package x\n" +
		"\n" +
		"// line comment body\n" +
		"/* block comment first line\n" +
		"   block comment continuation line\n" +
		"*/\n" +
		"const banner = \"// looks like a comment but is data\"\n" +
		"const raw = `// raw string data`\n" +
		"const r = '/'\n" +
		"func f() { println(banner, raw, r) }\n"
	assertProjection(t, "comments-only-go", src,
		[]string{"line comment body", "block comment first line", "block comment continuation line"},
		[]string{"package x", "looks like a comment but is data", "raw string data", "func f()"})
}

func TestCommentsOnlyDart(t *testing.T) {
	src := "class X {\n" +
		"  /// doc comment body\n" +
		"  /* outer /* nested */ still inside the outer block */\n" +
		"  final a = '// single quoted data';\n" +
		"  final b = \"\"\"\n" +
		"// triple quoted data\n" +
		"\"\"\";\n" +
		"  final c = r'// raw string data';\n" +
		"}\n"
	assertProjection(t, "comments-only-dart", src,
		[]string{"doc comment body", "nested", "still inside the outer block"},
		[]string{"class X", "single quoted data", "triple quoted data", "raw string data"})
}

func TestCommentsOnlySQL(t *testing.T) {
	src := "-- line comment body\n" +
		"/* block comment first line\n" +
		"   block comment continuation line\n" +
		"*/\n" +
		"INSERT INTO t (a, b, c) VALUES ('-- single quoted data', E'-- escape string data', 1);\n" +
		"CREATE FUNCTION f() RETURNS int AS $fn$\n" +
		"-- dollar quoted body data\n" +
		"$fn$ LANGUAGE sql;\n" +
		"SELECT \"-- quoted identifier data\" FROM t;\n"
	assertProjection(t, "comments-only-sql", src,
		[]string{"line comment body", "block comment first line", "block comment continuation line"},
		[]string{"INSERT INTO t", "single quoted data", "escape string data",
			"dollar quoted body data", "quoted identifier data"})
}
