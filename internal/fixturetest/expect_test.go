package fixturetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectExpectationsFindsInlineMarkers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fire-1")
	writeTree(t, dir, map[string]string{
		"src/a.txt": "clean\nbad line want: my-rule\nalso clean\n",
		"src/b.txt": "bad want: other-rule\n",
	})
	fset, err := scan.Walk(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := collectExpectations(fset, dir, "my-rule")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (expectation{Path: "src/a.txt", Line: 2}) {
		t.Fatalf("expectations: %+v", got)
	}
}

func TestCollectExpectationsReadsWantManifest(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "fire-1")
	writeTree(t, dir, map[string]string{"x.md": "content\n"})
	manifest := "# comment\n\n-\nx.md\ndocs/y.md:7\n"
	if err := os.WriteFile(dir+".want", []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := collectExpectations(fset, dir, "my-rule")
	if err != nil {
		t.Fatal(err)
	}
	want := []expectation{{}, {Path: "x.md"}, {Path: "docs/y.md", Line: 7}}
	if len(got) != len(want) {
		t.Fatalf("expectations: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expectation %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestCollectExpectationsRejectsBadManifestLine(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "fire-1")
	writeTree(t, dir, map[string]string{"x.md": "content\n"})
	if err := os.WriteFile(dir+".want", []byte("docs/y.md:notanumber\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collectExpectations(fset, dir, "my-rule"); err == nil || !strings.Contains(err.Error(), "notanumber") {
		t.Fatalf("bad manifest line accepted: %v", err)
	}
}

func TestDiffReportsMissingAndUnexpected(t *testing.T) {
	findings := []finding.Finding{
		{Path: "a.txt", Line: 2},
		{Path: "c.txt", Line: 9},
	}
	expected := []expectation{{Path: "a.txt", Line: 2}, {Path: "b.txt", Line: 1}}
	problems := diff(findings, expected)
	if len(problems) != 2 {
		t.Fatalf("problems: %v", problems)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "missing expected finding b.txt:1") ||
		!strings.Contains(joined, "unexpected finding c.txt:9") {
		t.Fatalf("problems: %v", problems)
	}
	if len(diff(findings[:1], expected[:1])) != 0 {
		t.Fatal("exact match reported problems")
	}
}

func TestExpectationString(t *testing.T) {
	cases := map[expectation]string{
		{}:                       "- (scope-level)",
		{Path: "a.txt"}:          "a.txt",
		{Path: "a.txt", Line: 3}: "a.txt:3",
	}
	for e, want := range cases {
		if e.String() != want {
			t.Fatalf("String(%+v) = %q, want %q", e, e.String(), want)
		}
	}
}

// Optional message substring on .want lines pins offender identity carried
// only in finding.Message (set-relation, pattern-count, etc.).
func TestCollectExpectationsReadsMessagePin(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "fire-1")
	writeTree(t, dir, map[string]string{"x.md": "content\n"})
	// Scope pin with message; path:line with message; bare - still allowed.
	manifest := "- missing from B: ghost\nsrc/a.go:3 countable free-ride\n-\n"
	if err := os.WriteFile(dir+".want", []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := collectExpectations(fset, dir, "my-rule")
	if err != nil {
		t.Fatal(err)
	}
	want := []expectation{
		{MessagePin: "missing from B: ghost"},
		{Path: "src/a.go", Line: 3, MessagePin: "countable free-ride"},
		{},
	}
	if len(got) != len(want) {
		t.Fatalf("expectations: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expectation %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestDiffRequiresMessageSubstringWhenPinned(t *testing.T) {
	// Finding fires at scope with wrong Message — must not satisfy a pinned want.
	findings := []finding.Finding{{Message: "generic scope error"}}
	expected := []expectation{{MessagePin: "missing from B: ghost"}}
	problems := diff(findings, expected)
	if len(problems) == 0 {
		t.Fatal("diff must fail when message pin is absent from finding.Message")
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "missing from B: ghost") && !strings.Contains(joined, "missing expected") {
		t.Fatalf("problem must name the unsatisfied pin:\n%s", joined)
	}

	// Right message → exact multiset match (no residual unexpected/missing).
	findings = []finding.Finding{{Message: "set-relation: missing from B: ghost"}}
	if problems = diff(findings, expected); len(problems) != 0 {
		t.Fatalf("matching message pin must pass, got %v", problems)
	}

	// Empty Message pin keeps Path+Line-only matching (back-compat).
	findings = []finding.Finding{{Path: "a.go", Line: 2, Message: "anything"}}
	expected = []expectation{{Path: "a.go", Line: 2}}
	if problems = diff(findings, expected); len(problems) != 0 {
		t.Fatalf("unpinned message must ignore finding.Message, got %v", problems)
	}
}

func TestExpectationStringIncludesMessagePin(t *testing.T) {
	e := expectation{Path: "a.go", Line: 3, MessagePin: "free-ride"}
	if got := e.String(); got != "a.go:3 message~free-ride" {
		t.Fatalf("String = %q", got)
	}
	e = expectation{MessagePin: "scope pin"}
	if got := e.String(); got != "- (scope-level) message~scope pin" {
		t.Fatalf("String = %q", got)
	}
}
