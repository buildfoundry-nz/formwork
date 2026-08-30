package filenaming_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup("file-naming")
	if !ok {
		t.Fatal("file-naming type not registered")
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
		t.Fatalf("build file-naming %q: %v", params, err)
	}
	return c
}

func check(t *testing.T, c rules.Checker, path string) []rules.Match {
	t.Helper()
	ms, err := c.CheckFile(scan.NewMemFile(path, []byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestForbiddenExtensionFires(t *testing.T) {
	c := build(t, "forbid_ext: ['.foo', '.bar']")
	ms := check(t, c, "pkg/thing.foo")
	if len(ms) != 1 || !strings.Contains(ms[0].Message, ".foo") {
		t.Fatalf("expected one finding naming .foo, got %+v", ms)
	}
	if got := check(t, c, "pkg/thing.go"); len(got) != 0 {
		t.Fatalf("clean extension should pass, got %+v", got)
	}
}

func TestRequireMatchMissFires(t *testing.T) {
	c := build(t, "require_match: '\\.go$'")
	if got := check(t, c, "cmd/main.go"); len(got) != 0 {
		t.Fatalf("path matching require_match should pass, got %+v", got)
	}
	ms := check(t, c, "docs/readme.md")
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "\\.go$") {
		t.Fatalf("expected one require_match finding, got %+v", ms)
	}
}

func TestReservedGlobHitFires(t *testing.T) {
	c := build(t, "reserved: ['secrets/**', '**/*.pem']")
	ms := check(t, c, "secrets/prod/key.txt")
	if len(ms) != 1 || !strings.Contains(ms[0].Message, "secrets/**") {
		t.Fatalf("expected one reserved finding naming the glob, got %+v", ms)
	}
	if got := check(t, c, "src/main.go"); len(got) != 0 {
		t.Fatalf("non-reserved path should pass, got %+v", got)
	}
}

func TestCleanFilePassesAllRules(t *testing.T) {
	c := build(t, "forbid_ext: ['.foo']\nrequire_match: '\\.go$'\nreserved: ['secrets/**']")
	if got := check(t, c, "internal/scan/scan.go"); len(got) != 0 {
		t.Fatalf("clean file should pass every rule, got %+v", got)
	}
}

func TestMultipleViolationsEmitOneMatchPerRule(t *testing.T) {
	c := build(t, "forbid_ext: ['.foo']\nrequire_match: '\\.go$'\nreserved: ['secrets/**']")
	// secrets/api.foo violates all three: forbidden ext, require_match miss, reserved hit.
	ms := check(t, c, "secrets/api.foo")
	if len(ms) != 3 {
		t.Fatalf("expected one finding per violated rule (3), got %d: %+v", len(ms), ms)
	}
}

func TestEmptyParamsRejected(t *testing.T) {
	f, ok := rules.Lookup("file-naming")
	if !ok {
		t.Fatal("file-naming type not registered")
	}
	if _, err := f(nil); err == nil {
		t.Fatal("nil params (no rule set) must be rejected")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("{}"), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("empty params mapping (no rule set) must be rejected")
	}
}

func TestBadParamsRejected(t *testing.T) {
	f, _ := rules.Lookup("file-naming")
	if _, err := f(paramsNode(t, "require_match: '('")); err == nil {
		t.Fatal("invalid require_match regex must be rejected")
	}
	if _, err := f(paramsNode(t, "reserved: ['[']")); err == nil {
		t.Fatal("invalid reserved glob must be rejected")
	}
	if _, err := f(paramsNode(t, "forbid_ext: ['foo']")); err == nil {
		t.Fatal("forbid_ext entry without a leading dot must be rejected")
	}
	if _, err := f(paramsNode(t, "bogus: true")); err == nil {
		t.Fatal("unknown param field must be rejected (strict decoding)")
	}
}

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}
