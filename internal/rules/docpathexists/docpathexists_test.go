package docpathexists_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// backtickPattern captures a repo-relative path token wrapped in backticks,
// e.g. `internal/thing.go`, with exactly one capturing group.
const backtickPattern = "pattern: '`([^`]+)`'\n"

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup("doc-path-exists")
	if !ok {
		t.Fatal("doc-path-exists type not registered")
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatalf("build %q: %v", params, err)
	}
	return c
}

func finalize(t *testing.T, c rules.Checker, root string) ([]rules.Match, error) {
	t.Helper()
	ef, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("doc-path-exists must implement ErrFinalizer")
	}
	return ef.FinalizeErr(rules.FinalizeContext{Root: root})
}

func TestCitingExistingPathPasses(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "internal", "thing.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package internal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := build(t, backtickPattern)
	if _, err := c.CheckFile(scan.NewMemFile("README.md", []byte("see `internal/thing.go` for details\n"))); err != nil {
		t.Fatal(err)
	}
	ms, err := finalize(t, c, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ms) != 0 {
		t.Fatalf("existing cited path should pass, got %+v", ms)
	}
}

func TestCitingMissingPathFailsAtCitingLocation(t *testing.T) {
	root := t.TempDir()
	c := build(t, backtickPattern)
	content := []byte("intro line\nsee `does/not/exist.go` here\n")
	if _, err := c.CheckFile(scan.NewMemFile("docs/guide.md", content)); err != nil {
		t.Fatal(err)
	}
	ms, err := finalize(t, c, root)
	if err != nil {
		t.Fatalf("a missing cited path is a finding, not an error: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("want one finding, got %+v", ms)
	}
	if ms[0].Path != "docs/guide.md" || ms[0].Line != 2 {
		t.Fatalf("finding should carry the citing location, got %+v", ms[0])
	}
	if !strings.Contains(ms[0].Message, "does/not/exist.go") {
		t.Fatalf("message should name the missing token, got %q", ms[0].Message)
	}
}

func TestDeterministicOrdering(t *testing.T) {
	root := t.TempDir()
	c := build(t, backtickPattern)
	// Insert citing files out of sorted order; every cited path is missing.
	// Findings must return sorted by (path, line) regardless of the order
	// CheckFile was called in (the engine drives it from a worker pool).
	if _, err := c.CheckFile(scan.NewMemFile("z.md", []byte("`missing/z1.go`\n"))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckFile(scan.NewMemFile("a.md", []byte("`missing/a1.go`\nmiddle\n`missing/a3.go`\n"))); err != nil {
		t.Fatal(err)
	}
	ms, err := finalize(t, c, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		path string
		line int
	}{
		{"a.md", 1}, {"a.md", 3}, {"z.md", 1},
	}
	if len(ms) != len(want) {
		t.Fatalf("want %d findings, got %+v", len(want), ms)
	}
	for i, w := range want {
		if ms[i].Path != w.path || ms[i].Line != w.line {
			t.Fatalf("finding %d: want %s:%d, got %s:%d", i, w.path, w.line, ms[i].Path, ms[i].Line)
		}
	}
}

func TestFastCost(t *testing.T) {
	c := build(t, backtickPattern)
	if rules.CostOf(c) != rules.CostFast {
		t.Fatalf("doc-path-exists must stay fast, got %q", rules.CostOf(c))
	}
	if _, ok := c.(rules.Coster); ok {
		t.Fatal("doc-path-exists must not implement Coster")
	}
}

func TestRejectsBadParams(t *testing.T) {
	factory, ok := rules.Lookup("doc-path-exists")
	if !ok {
		t.Fatal("doc-path-exists type not registered")
	}
	if _, err := factory(nil); err == nil {
		t.Fatal("missing pattern must be rejected")
	}
	if _, err := factory(paramsNode(t, "pattern: '('\n")); err == nil {
		t.Fatal("invalid regex must be rejected")
	}
	if _, err := factory(paramsNode(t, "pattern: 'nogroup'\n")); err == nil {
		t.Fatal("a pattern with no capturing group must be rejected")
	}
	if _, err := factory(paramsNode(t, "pattern: '(a)(b)'\n")); err == nil {
		t.Fatal("a pattern with more than one capturing group must be rejected")
	}
	if _, err := factory(paramsNode(t, "pattern: x\nbogus: 1\n")); err == nil {
		t.Fatal("unknown param field must be rejected")
	}
}
