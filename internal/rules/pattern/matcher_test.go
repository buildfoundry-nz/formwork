package pattern_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// backtrackBomb is a line that drives (a+)+$ into catastrophic backtracking:
// many 'a's followed by a non-'a' char means the anchored match can never
// succeed, so the regexp2 engine explores ~2^n paths and trips its 1s cap.
var backtrackBomb = strings.Repeat("a", 50) + "!"

// TestForbiddenRegexp2TimeoutFailsClosed: a regexp2 match timeout on the
// per-line path must surface as an evaluation error (→ exit 2), never a silent
// no-match that reads as a clean pass (#22).
func TestForbiddenRegexp2TimeoutFailsClosed(t *testing.T) {
	t.Parallel() // ~1s on the hardcoded 1s match timeout — overlap it with its siblings
	c := mkForbidden(t, "pattern: '(a+)+$'\nsyntax: regexp2")
	if _, err := c.CheckFile(scan.NewMemFile("a.txt", []byte(backtrackBomb+"\n"))); err == nil {
		t.Fatal("regexp2 match timeout must surface as an error, got nil (silent no-match)")
	}
}

// TestForbiddenRegexp2MultilineTimeoutFailsClosed: same, on the whole-file
// FindLine path used by multiline: true.
func TestForbiddenRegexp2MultilineTimeoutFailsClosed(t *testing.T) {
	t.Parallel() // ~1s on the hardcoded 1s match timeout — overlap it with its siblings
	c := mkForbidden(t, "pattern: '(a+)+$'\nsyntax: regexp2\nmultiline: true")
	if _, err := c.CheckFile(scan.NewMemFile("a.txt", []byte(backtrackBomb))); err == nil {
		t.Fatal("regexp2 multiline match timeout must surface as an error, got nil")
	}
}

func mkForbidden(t *testing.T, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup("forbidden-pattern")
	if !ok {
		t.Fatal("forbidden-pattern not registered")
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
		t.Fatalf("build %q: %v", params, err)
	}
	return c
}

func TestForbiddenRegexp2NegativeLookahead(t *testing.T) {
	// PCRE2 negative lookahead — impossible in RE2. Ban `skip:` unless it is
	// immediately followed by ` allow`.
	c := mkForbidden(t, "pattern: 'skip:(?! allow)'\nsyntax: regexp2")
	if m, _ := c.CheckFile(scan.NewMemFile("a.dart", []byte("t('x', skip: true);\n"))); len(m) != 1 {
		t.Fatalf("regexp2 lookahead should fire on 'skip: true', got %v", m)
	}
	if m, _ := c.CheckFile(scan.NewMemFile("b.dart", []byte("t('x', skip: allow);\n"))); len(m) != 0 {
		t.Fatalf("regexp2 lookahead should exclude 'skip: allow', got %v", m)
	}
}

func TestForbiddenRe2DefaultStillWorks(t *testing.T) {
	c := mkForbidden(t, "pattern: 'WIDGET'") // no syntax → RE2 (token avoids the self-host marker-comment gate)
	if m, _ := c.CheckFile(scan.NewMemFile("a.go", []byte("var x = WIDGET\n"))); len(m) != 1 {
		t.Fatalf("default RE2 should still match, got %v", m)
	}
}

func TestForbiddenMultilineCrossLine(t *testing.T) {
	// (?s) lets . cross newlines; multiline matches over whole file content.
	c := mkForbidden(t, "pattern: '(?s)Stack\\(.*InteractiveViewer\\('\nmultiline: true")
	fire := "Widget b() {\n  return Stack(children: [\n    InteractiveViewer(child: x),\n  ]);\n}\n"
	if m, _ := c.CheckFile(scan.NewMemFile("a.dart", []byte(fire))); len(m) != 1 || m[0].Line != 2 {
		t.Fatalf("multiline should match across lines at line 2, got %v", m)
	}
	pass := "Widget b() {\n  return Stack(children: []);\n}\n" // no InteractiveViewer
	if m, _ := c.CheckFile(scan.NewMemFile("b.dart", []byte(pass))); len(m) != 0 {
		t.Fatalf("multiline should not match without both patterns, got %v", m)
	}
}

func TestForbiddenAllOfCoOccurrence(t *testing.T) {
	c := mkForbidden(t, "all_of: ['InteractiveViewer', 'Stack']")
	fire := "class W {\n  InteractiveViewer()\n  Stack()\n}\n"
	if m, _ := c.CheckFile(scan.NewMemFile("a.dart", []byte(fire))); len(m) != 1 {
		t.Fatalf("both present should fire, got %v", m)
	}
	if m, _ := c.CheckFile(scan.NewMemFile("b.dart", []byte("class W {\n  Stack()\n}\n"))); len(m) != 0 {
		t.Fatalf("only one present should pass, got %v", m)
	}
}

func TestForbiddenAllOfWithNoneOf(t *testing.T) {
	c := mkForbidden(t, "all_of: ['jsonDecode']\nnone_of: ['fromJson']")
	if m, _ := c.CheckFile(scan.NewMemFile("a.dart", []byte("jsonDecode(x)\n"))); len(m) != 1 {
		t.Fatalf("all_of present + none_of absent should fire, got %v", m)
	}
	if m, _ := c.CheckFile(scan.NewMemFile("b.dart", []byte("jsonDecode(x).fromJson()\n"))); len(m) != 0 {
		t.Fatalf("none_of present should pass, got %v", m)
	}
}

func TestForbiddenPrefilterGate(t *testing.T) {
	// prefilter literal absent → skip cheaply even though the pattern would match
	c := mkForbidden(t, "pattern: 'InteractiveViewer'\nmultiline: true\nprefilter: 'ZZZ_NOT_PRESENT'")
	if m, _ := c.CheckFile(scan.NewMemFile("a.dart", []byte("InteractiveViewer()\n"))); len(m) != 0 {
		t.Fatalf("absent prefilter should skip, got %v", m)
	}
	// prefilter present → normal match
	c2 := mkForbidden(t, "pattern: 'InteractiveViewer'\nmultiline: true\nprefilter: 'Interactive'")
	if m, _ := c2.CheckFile(scan.NewMemFile("a.dart", []byte("InteractiveViewer()\n"))); len(m) != 1 {
		t.Fatalf("present prefilter should match, got %v", m)
	}
}

func TestForbiddenPatternXorAllOfRequired(t *testing.T) {
	f, _ := rules.Lookup("forbidden-pattern")
	var d yaml.Node
	_ = yaml.Unmarshal([]byte("severity: error"), &d)
	if _, err := f(d.Content[0]); err == nil {
		t.Error("neither pattern nor all_of should error")
	}
	var d2 yaml.Node
	_ = yaml.Unmarshal([]byte("pattern: x\nall_of: [y]"), &d2)
	if _, err := f(d2.Content[0]); err == nil {
		t.Error("both pattern and all_of should error")
	}
}

func TestForbiddenUnknownSyntaxRejected(t *testing.T) {
	f, _ := rules.Lookup("forbidden-pattern")
	var doc yaml.Node
	_ = yaml.Unmarshal([]byte("pattern: x\nsyntax: pcre"), &doc)
	if _, err := f(doc.Content[0]); err == nil {
		t.Fatal("unknown syntax must be rejected")
	}
}
