package goast_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

const declaredOnceType = "go/declared-once"

func declaredOnce(t *testing.T, decls ...string) rules.Checker {
	t.Helper()
	var b strings.Builder
	b.WriteString("declarations:\n")
	for i := 0; i+1 < len(decls); i += 2 {
		b.WriteString("  - path: " + decls[i] + "\n    decl: " + decls[i+1] + "\n")
	}
	return build(t, declaredOnceType, b.String())
}

// runAll feeds every file to c and returns the per-file matches plus Finalize.
func runAll(t *testing.T, c rules.Checker, files map[string]string) []rules.Match {
	t.Helper()
	var out []rules.Match
	for path, src := range files {
		ms, err := c.CheckFile(scan.NewMemFile(path, []byte(src)))
		if err != nil {
			t.Fatalf("CheckFile(%s): %v", path, err)
		}
		out = append(out, ms...)
	}
	return append(out, c.(rules.Finalizer).Finalize()...)
}

const owner = `package widgets

func render(n int) int { return n }
`

func TestDeclaredOnceAloneIsClean(t *testing.T) {
	c := declaredOnce(t, "a/widgets.go", "func render")
	if got := runAll(t, c, map[string]string{"a/widgets.go": owner}); len(got) != 0 {
		t.Fatalf("one listed declaration in its owner file is clean: %+v", got)
	}
}

func TestSecondDeclarationAnywhereFails(t *testing.T) {
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func render"), map[string]string{
		"a/widgets.go": owner,
		"b/copy.go":    "package copy\n\nfunc render(n int) int { return n + 1 }\n",
	})
	if len(got) != 1 || got[0].Path != "b/copy.go" || got[0].Line != 3 {
		t.Fatalf("the second declaration is the finding: %+v", got)
	}
	if !strings.Contains(got[0].Message, "func render is declared once, at a/widgets.go:3") {
		t.Fatalf("the finding names the owner: %q", got[0].Message)
	}
}

func TestSecondDeclarationInTheOwnerFileFails(t *testing.T) {
	src := owner + "\nfunc render(n int) int { return n }\n"
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func render"), map[string]string{"a/widgets.go": src})
	if len(got) != 1 || got[0].Line != 5 {
		t.Fatalf("a duplicate beside its owner is still a duplicate: %+v", got)
	}
}

func TestUnlistedDuplicatesAreNotItsBusiness(t *testing.T) {
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func render"), map[string]string{
		"a/widgets.go": owner,
		"a/x.go":       "package a\n\nfunc helper() {}\n",
		"b/y.go":       "package b\n\nfunc helper() {}\n",
	})
	if len(got) != 0 {
		t.Fatalf("only a listed identity is held to one declaration: %+v", got)
	}
}

func TestKindsAreDistinctIdentities(t *testing.T) {
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func render"), map[string]string{
		"a/widgets.go": owner,
		"b/y.go":       "package b\n\nvar render = 1\n\ntype T struct{}\n\nfunc (T) render() {}\n",
	})
	if len(got) != 0 {
		t.Fatalf("a var or a method named like a listed func is a different declaration: %+v", got)
	}
}

func TestListedMethodIsKeyedOnItsReceiver(t *testing.T) {
	src := "package c\n\ntype Client struct{}\n\nfunc (c *Client) pump() {}\n"
	got := runAll(t, declaredOnce(t, "c/client.go", "method Client.pump"), map[string]string{
		"c/client.go": src,
		"d/other.go":  "package d\n\ntype Other struct{}\n\nfunc (o Other) pump() {}\n",
		"e/copy.go":   "package e\n\ntype Client struct{}\n\nfunc (c Client) pump() {}\n",
	})
	if len(got) != 1 || got[0].Path != "e/copy.go" {
		t.Fatalf("only a second Client.pump duplicates it: %+v", got)
	}
}

func TestListedConstAndTypeSpecs(t *testing.T) {
	src := "package p\n\nconst Label = \"auto\"\n\ntype Palette struct{}\n"
	got := runAll(t, declaredOnce(t, "p/p.go", "const Label", "p/p.go", "type Palette"), map[string]string{
		"p/p.go": src,
		"q/q.go": "package q\n\nconst Label = \"auto\"\n\ntype Palette int\n",
	})
	if len(got) != 2 {
		t.Fatalf("a listed const and a listed type are each declared once: %+v", got)
	}
}

func TestGroupedSpecsAreListedOneByOne(t *testing.T) {
	src := "package p\n\nconst (\n\tLabel = \"auto\"\n\tOther = \"x\"\n)\n"
	got := runAll(t, declaredOnce(t, "p/p.go", "const Label"), map[string]string{
		"p/p.go": src,
		"q/q.go": "package q\n\nconst Other = \"x\"\n",
	})
	if len(got) != 0 {
		t.Fatalf("listing one spec of a group holds that spec, not its siblings: %+v", got)
	}
}

func TestListedDeclarationMissingFromItsFileFails(t *testing.T) {
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func paint"), map[string]string{"a/widgets.go": owner})
	if len(got) != 1 || got[0].Path != "a/widgets.go" || !strings.Contains(got[0].Message, "declares no top-level func paint") {
		t.Fatalf("a listed declaration its file does not declare is stale, and says so: %+v", got)
	}
}

func TestListedDeclarationMovedElsewhereFails(t *testing.T) {
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func render"), map[string]string{
		"a/widgets.go": "package widgets\n",
		"b/moved.go":   "package moved\n\nfunc render(n int) int { return n }\n",
	})
	if len(got) != 2 {
		t.Fatalf("a declaration that left its listed file is stale there and misplaced here: %+v", got)
	}
}

func TestListedFileOutsideTheTreeIsNotJudged(t *testing.T) {
	got := runAll(t, declaredOnce(t, "a/widgets.go", "func render", "z/absent.go", "func paint"), map[string]string{"a/widgets.go": owner})
	if len(got) != 0 {
		t.Fatalf("a listed file the tree does not hold says nothing about staleness: %+v", got)
	}
	got = runAll(t, declaredOnce(t, "z/absent.go", "func render"), map[string]string{"b/copy.go": "package copy\n\nfunc render() {}\n"})
	if len(got) != 0 {
		t.Fatalf("with its owner file absent there is no owner to duplicate: %+v", got)
	}
}

func TestDeclaredOnceConfig(t *testing.T) {
	for _, tc := range []struct{ name, yaml string }{
		{"no declarations", ""},
		{"empty list", "declarations: []"},
		{"blank path", "declarations:\n  - path: ' '\n    decl: func render"},
		{"unknown kind", "declarations:\n  - path: a.go\n    decl: function render"},
		{"bare name", "declarations:\n  - path: a.go\n    decl: render"},
		{"method without receiver", "declarations:\n  - path: a.go\n    decl: method render"},
		{"listed twice", "declarations:\n  - path: a.go\n    decl: func render\n  - path: b.go\n    decl: func render"},
	} {
		if err := buildErr(t, declaredOnceType, tc.yaml); err == nil {
			t.Errorf("%s: must be a config error", tc.name)
		}
	}
}

func TestDeclaredOnceIsAScopeBoundInvariant(t *testing.T) {
	c := declaredOnce(t, "a/widgets.go", "func render")
	if w, ok := c.(rules.WholeTreeInvariant); !ok || !w.WholeTreeInvariant() {
		t.Fatal("it counts across files, so it judges the whole tree")
	}
	if !rules.IsScopeBound(c) {
		t.Fatal("it reads only the files its scope admits")
	}
}
