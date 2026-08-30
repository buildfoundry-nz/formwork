package patterncount_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
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

func mustChecker(t *testing.T, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup("pattern-count")
	if !ok {
		t.Fatal("type \"pattern-count\" not registered")
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// feed counts pattern matches across the given in-memory files, then finalizes.
func feed(t *testing.T, c rules.Checker, files ...*scan.File) []rules.Match {
	t.Helper()
	for _, f := range files {
		if _, err := c.CheckFile(f); err != nil {
			t.Fatal(err)
		}
	}
	fin, ok := c.(rules.Finalizer)
	if !ok {
		t.Fatal("pattern-count does not implement rules.Finalizer")
	}
	return fin.Finalize()
}

func file(name, body string) *scan.File { return scan.NewMemFile(name, []byte(body)) }

func TestExactlyPasses(t *testing.T) {
	c := mustChecker(t, "pattern: 'TODO'\nop: exactly\nn: 2\n")
	ms := feed(t, c,
		file("a.go", "x\nTODO one\ny\n"),
		file("b.go", "TODO two\n"),
	)
	if len(ms) != 0 {
		t.Fatalf("exactly 2 with 2 matches should pass: %+v", ms)
	}
}

func TestExactlyFails(t *testing.T) {
	c := mustChecker(t, "pattern: 'TODO'\nop: exactly\nn: 2\n")
	ms := feed(t, c,
		file("a.go", "TODO one\nTODO two\n"),
		file("b.go", "TODO three\n"),
	)
	if len(ms) != 1 || ms[0].Path != "" || ms[0].Line != 0 {
		t.Fatalf("want one scope-level match, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "exactly") || !strings.Contains(ms[0].Message, "2") || !strings.Contains(ms[0].Message, "3") {
		t.Fatalf("message must state op, n, and actual count: %q", ms[0].Message)
	}
}

func TestAtMostPasses(t *testing.T) {
	c := mustChecker(t, "pattern: 'FIXME'\nop: at-most\nn: 3\n")
	ms := feed(t, c,
		file("a.go", "FIXME\nFIXME\n"),
	)
	if len(ms) != 0 {
		t.Fatalf("at-most 3 with 2 matches should pass: %+v", ms)
	}
}

func TestAtMostFails(t *testing.T) {
	c := mustChecker(t, "pattern: 'FIXME'\nop: at-most\nn: 1\n")
	ms := feed(t, c,
		file("a.go", "FIXME\nFIXME\nFIXME\n"),
	)
	if len(ms) != 1 {
		t.Fatalf("at-most 1 with 3 matches should fail: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "at-most") || !strings.Contains(ms[0].Message, "3") {
		t.Fatalf("message must state op and actual count: %q", ms[0].Message)
	}
}

func TestAtLeastPasses(t *testing.T) {
	c := mustChecker(t, "pattern: 'license'\nop: at-least\nn: 2\n")
	ms := feed(t, c,
		file("a.go", "license\n"),
		file("b.go", "license\nlicense\n"),
	)
	if len(ms) != 0 {
		t.Fatalf("at-least 2 with 3 matches should pass: %+v", ms)
	}
}

func TestAtLeastFails(t *testing.T) {
	c := mustChecker(t, "pattern: 'license'\nop: at-least\nn: 5\n")
	ms := feed(t, c,
		file("a.go", "license\n"),
	)
	if len(ms) != 1 {
		t.Fatalf("at-least 5 with 1 match should fail: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "at-least") || !strings.Contains(ms[0].Message, "5") || !strings.Contains(ms[0].Message, "1") {
		t.Fatalf("message must state op, n, and actual count: %q", ms[0].Message)
	}
}

// A line matching the pattern more than once counts once: matches are counted
// per matching line, not per occurrence.
func TestCountsMatchingLinesNotOccurrences(t *testing.T) {
	c := mustChecker(t, "pattern: 'ab'\nop: exactly\nn: 1\n")
	ms := feed(t, c, file("a.go", "ab ab ab\n"))
	if len(ms) != 0 {
		t.Fatalf("a line matching thrice should count once: %+v", ms)
	}
}

func TestExactlyZeroPassesOnNoMatches(t *testing.T) {
	c := mustChecker(t, "pattern: 'never'\nop: exactly\nn: 0\n")
	ms := feed(t, c, file("a.go", "clean\n"))
	if len(ms) != 0 {
		t.Fatalf("exactly 0 with 0 matches should pass: %+v", ms)
	}
}

func TestRejectsBadOp(t *testing.T) {
	factory, _ := rules.Lookup("pattern-count")
	if _, err := factory(paramsNode(t, "pattern: x\nop: sometimes\nn: 1\n")); err == nil {
		t.Fatal("unknown op accepted")
	}
	if _, err := factory(paramsNode(t, "pattern: x\nn: 1\n")); err == nil {
		t.Fatal("missing op accepted")
	}
}

func TestRejectsMissingPattern(t *testing.T) {
	factory, _ := rules.Lookup("pattern-count")
	if _, err := factory(paramsNode(t, "op: exactly\nn: 1\n")); err == nil {
		t.Fatal("missing pattern accepted")
	}
	if _, err := factory(paramsNode(t, "pattern: '('\nop: exactly\nn: 1\n")); err == nil {
		t.Fatal("invalid regex accepted")
	}
}

func TestRejectsBadN(t *testing.T) {
	factory, _ := rules.Lookup("pattern-count")
	if _, err := factory(paramsNode(t, "pattern: x\nop: exactly\n")); err == nil {
		t.Fatal("missing n accepted")
	}
	if _, err := factory(paramsNode(t, "pattern: x\nop: exactly\nn: -1\n")); err == nil {
		t.Fatal("negative n accepted")
	}
}

func TestRejectsUnknownField(t *testing.T) {
	factory, _ := rules.Lookup("pattern-count")
	if _, err := factory(paramsNode(t, "pattern: x\nop: exactly\nn: 1\nbogus: true\n")); err == nil {
		t.Fatal("unknown param field accepted")
	}
}

// TestWholeTreeInvariant pins the #4 mechanism: a pattern-count compares a
// scope-wide match total to n, so it must be evaluated whole-tree under --range
// rather than range-scoped (which would tally only changed files and report the
// wrong count).
func TestWholeTreeInvariant(t *testing.T) {
	c := mustChecker(t, "pattern: 'TODO'\nop: exactly\nn: 2\n")
	if !rules.IsWholeTreeInvariant(c) {
		t.Fatal("pattern-count must be a whole-tree invariant")
	}
}
