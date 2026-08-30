package binarycontent_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func mustChecker(t *testing.T, typeName, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup(typeName)
	if !ok {
		t.Fatalf("type %q not registered", typeName)
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBinaryContentOversizedFileFlagged(t *testing.T) {
	c := mustChecker(t, "binary-content", "max_bytes: 4\n")
	ms, err := c.CheckFile(scan.NewMemFile("big.txt", []byte("hello world")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("matches: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "max_bytes") {
		t.Fatalf("message: %q", ms[0].Message)
	}
}

func TestBinaryContentSizeAtLimitPasses(t *testing.T) {
	c := mustChecker(t, "binary-content", "max_bytes: 5\n")
	ms, err := c.CheckFile(scan.NewMemFile("edge.txt", []byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("size at the limit must pass: %+v", ms)
	}
}

func TestBinaryContentNulByteFlaggedWhenForbidBinary(t *testing.T) {
	c := mustChecker(t, "binary-content", "forbid_binary: true\n")
	ms, err := c.CheckFile(scan.NewMemFile("blob.bin", []byte("abc\x00def")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("matches: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "binary") {
		t.Fatalf("message: %q", ms[0].Message)
	}
}

func TestBinaryContentTextFilePasses(t *testing.T) {
	c := mustChecker(t, "binary-content", "max_bytes: 4096\nforbid_binary: true\n")
	ms, err := c.CheckFile(scan.NewMemFile("ok.go", []byte("package main\n\nfunc main() {}\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("clean text file flagged: %+v", ms)
	}
}

func TestBinaryContentNulBeyondSniffWindowNotBinary(t *testing.T) {
	// The heuristic only inspects the first 8000 bytes; a NUL past that is
	// not treated as binary.
	content := append(make([]byte, 8000), 0)
	for i := range content[:8000] {
		content[i] = 'x'
	}
	c := mustChecker(t, "binary-content", "forbid_binary: true\n")
	ms, err := c.CheckFile(scan.NewMemFile("late.txt", content))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("NUL beyond the sniff window must not flag: %+v", ms)
	}
}

func TestBinaryContentBothViolationsEmitted(t *testing.T) {
	c := mustChecker(t, "binary-content", "max_bytes: 2\nforbid_binary: true\n")
	ms, err := c.CheckFile(scan.NewMemFile("both.bin", []byte("ab\x00cd")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("want size + binary findings, got: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "max_bytes") || !strings.Contains(ms[1].Message, "binary") {
		t.Fatalf("unexpected order/messages: %+v", ms)
	}
}

func TestBinaryContentRejectsEmptyAndBadParams(t *testing.T) {
	factory, ok := rules.Lookup("binary-content")
	if !ok {
		t.Fatal("type not registered")
	}
	if _, err := factory(nil); err == nil {
		t.Fatal("empty params accepted")
	}
	if _, err := factory(paramsNode(t, "forbid_binary: false\n")); err == nil {
		t.Fatal("no-op params (neither field set) accepted")
	}
	if _, err := factory(paramsNode(t, "max_bytes: -1\n")); err == nil {
		t.Fatal("negative max_bytes accepted")
	}
	if _, err := factory(paramsNode(t, "bogus: 1\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
}
