package ordering_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/ordering"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup("ordering")
	if !ok {
		t.Fatal("ordering type not registered")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	var node *yaml.Node
	if len(doc.Content) > 0 {
		node = doc.Content[0]
	}
	c, err := f(node)
	if err != nil {
		t.Fatalf("build ordering %q: %v", params, err)
	}
	return c
}

func TestOrderingCorrectOrderPasses(t *testing.T) {
	c := build(t, "before: 'package '\nafter: 'func '\n")
	f := scan.NewMemFile("main.go", []byte("package main\n\nimport \"x\"\n\nfunc main() {}\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("ordered file flagged: %+v", ms)
	}
}

func TestOrderingReversedFailsAtAfterLine(t *testing.T) {
	c := build(t, "before: 'package '\nafter: 'func '\n")
	// after ('func ') appears on line 1, before ('package ') on line 3.
	f := scan.NewMemFile("main.go", []byte("func early() {}\n\npackage main\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 1 {
		t.Fatalf("want one finding at line 1, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "func ") {
		t.Fatalf("message should name the after pattern: %q", ms[0].Message)
	}
}

func TestOrderingOnlyBeforePresentPasses(t *testing.T) {
	c := build(t, "before: 'package '\nafter: 'func '\n")
	ms, err := c.CheckFile(scan.NewMemFile("main.go", []byte("package main\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("only-before file flagged: %+v", ms)
	}
}

func TestOrderingOnlyAfterPresentPasses(t *testing.T) {
	c := build(t, "before: 'package '\nafter: 'func '\n")
	ms, err := c.CheckFile(scan.NewMemFile("main.go", []byte("func lonely() {}\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("only-after file flagged: %+v", ms)
	}
}

func TestOrderingSameLinePasses(t *testing.T) {
	// after_index is not strictly less than before_index, so no violation.
	c := build(t, "before: 'before'\nafter: 'after'\n")
	ms, err := c.CheckFile(scan.NewMemFile("x.txt", []byte("before after\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("same-line occurrence flagged: %+v", ms)
	}
}

func TestOrderingWithinFileExplicitPasses(t *testing.T) {
	c := build(t, "before: 'a'\nafter: 'b'\nwithin: file\n")
	if c == nil {
		t.Fatal("explicit within: file rejected")
	}
}

func TestOrderingRejectsUnknownWithin(t *testing.T) {
	f, _ := rules.Lookup("ordering")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("before: a\nafter: b\nwithin: repo\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("unknown within value accepted")
	}
}

func TestOrderingRejectsMissingParams(t *testing.T) {
	f, _ := rules.Lookup("ordering")
	if _, err := f(nil); err == nil {
		t.Fatal("missing before/after accepted")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("before: 'x'\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("missing after accepted")
	}
}

func TestOrderingRejectsBadRegex(t *testing.T) {
	f, _ := rules.Lookup("ordering")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("before: '('\nafter: 'b'\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("invalid before regex accepted")
	}
}

func TestOrderingRejectsUnknownField(t *testing.T) {
	f, _ := rules.Lookup("ordering")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("before: a\nafter: b\nnope: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("unknown param field accepted")
	}
}
