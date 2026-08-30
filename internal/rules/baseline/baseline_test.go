package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/baseline"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// build instantiates the baseline checker from YAML params, failing the test
// if construction errors.
func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	c, err := buildErr(t, params)
	if err != nil {
		t.Fatalf("build baseline %q: %v", params, err)
	}
	return c
}

// buildErr instantiates the baseline checker and returns any construction error
// (used to assert param validation).
func buildErr(t *testing.T, params string) (rules.Checker, error) {
	t.Helper()
	f, ok := rules.Lookup("baseline")
	if !ok {
		t.Fatal("baseline type not registered")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	var node *yaml.Node
	if len(doc.Content) > 0 {
		node = doc.Content[0]
	}
	return f(node)
}

// run feeds the given files through CheckFile, writes baselineContent to
// baselineRel under a fresh temp root, and returns the finalizer verdict.
func run(t *testing.T, c rules.Checker, baselineRel, baselineContent string, files map[string]string) ([]rules.Match, error) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, baselineRel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(baselineContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return finalize(t, c, root, files)
}

// finalize feeds files through CheckFile and calls FinalizeErr against root.
func finalize(t *testing.T, c rules.Checker, root string, files map[string]string) ([]rules.Match, error) {
	t.Helper()
	for path, content := range files {
		if _, err := c.CheckFile(scan.NewMemFile(path, []byte(content))); err != nil {
			t.Fatalf("CheckFile %s: %v", path, err)
		}
	}
	ef, ok := c.(rules.ErrFinalizer)
	if !ok {
		t.Fatal("baseline should implement ErrFinalizer")
	}
	return ef.FinalizeErr(rules.FinalizeContext{Root: root})
}

const exactParams = "pattern: '^allow (\\w+)$'\nbaseline: baseline.txt\nmode: exact"
const shrinkParams = "pattern: '^allow (\\w+)$'\nbaseline: baseline.txt\nmode: shrink-only"

func TestExactEqualPasses(t *testing.T) {
	c := build(t, exactParams)
	// Baseline includes blank + comment lines, which must be ignored.
	m, err := run(t, c, "baseline.txt", "# allowed\nalpha\n\nbeta\n", map[string]string{
		"a.txt": "allow alpha",
		"b.txt": "allow beta",
	})
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("exact set equal to baseline should pass, got %v", m)
	}
}

func TestExactDiffFails(t *testing.T) {
	c := build(t, exactParams)
	// Current = {alpha, gamma}; baseline = {alpha, beta}.
	// Expect one addition (gamma) AND one removal (beta) reported.
	m, err := run(t, c, "baseline.txt", "alpha\nbeta\n", map[string]string{
		"a.txt": "allow alpha",
		"g.txt": "allow gamma",
	})
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 findings (one addition, one removal), got %d: %v", len(m), m)
	}
	joined := m[0].Message + "\n" + m[1].Message
	if !strings.Contains(joined, "gamma") || !strings.Contains(joined, "beta") {
		t.Fatalf("expected findings naming gamma (added) and beta (removed), got %v", m)
	}
}

func TestShrinkOnlyAllowsRemovals(t *testing.T) {
	c := build(t, shrinkParams)
	// Current = {alpha}; baseline = {alpha, beta}. beta removed → allowed.
	m, err := run(t, c, "baseline.txt", "alpha\nbeta\n", map[string]string{
		"a.txt": "allow alpha",
	})
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("shrink-only must allow removals, got %v", m)
	}
}

func TestShrinkOnlyFailsOnAdditions(t *testing.T) {
	c := build(t, shrinkParams)
	// Current = {alpha, gamma}; baseline = {alpha}. gamma added → fails.
	m, err := run(t, c, "baseline.txt", "alpha\n", map[string]string{
		"a.txt": "allow alpha",
		"g.txt": "allow gamma",
	})
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 1 || !strings.Contains(m[0].Message, "gamma") {
		t.Fatalf("shrink-only must fail on the added value gamma, got %v", m)
	}
}

func TestMissingBaselineIsError(t *testing.T) {
	c := build(t, exactParams)
	root := t.TempDir() // baseline.txt intentionally absent
	_, err := finalize(t, c, root, map[string]string{"a.txt": "allow alpha"})
	if err == nil {
		t.Fatal("a missing baseline file must be an engine error, not a silent pass")
	}
}

func TestDeterministicSortedAdditions(t *testing.T) {
	c := build(t, shrinkParams)
	// Multiple additions must come back sorted.
	m, err := run(t, c, "baseline.txt", "", map[string]string{
		"f.txt": "allow gamma\nallow alpha\nallow beta",
	})
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}
	if len(m) != 3 {
		t.Fatalf("expected 3 additions, got %d: %v", len(m), m)
	}
	if !strings.Contains(m[0].Message, "alpha") ||
		!strings.Contains(m[1].Message, "beta") ||
		!strings.Contains(m[2].Message, "gamma") {
		t.Fatalf("additions must be sorted (alpha, beta, gamma), got %v", m)
	}
}

func TestStaysFastCost(t *testing.T) {
	c := build(t, exactParams)
	if rules.CostOf(c) != rules.CostFast {
		t.Fatalf("baseline must stay fast cost, got %q", rules.CostOf(c))
	}
}

func TestMissingPatternIsConfigError(t *testing.T) {
	if _, err := buildErr(t, "baseline: b.txt\nmode: exact"); err == nil {
		t.Fatal("missing pattern must be a config error")
	}
}

func TestMissingModeIsConfigError(t *testing.T) {
	if _, err := buildErr(t, "pattern: '(\\w+)'\nbaseline: b.txt"); err == nil {
		t.Fatal("missing/invalid mode must be a config error")
	}
}

func TestUnknownParamIsError(t *testing.T) {
	if _, err := buildErr(t, "pattern: '(\\w+)'\nbaseline: b.txt\nmode: exact\nbogus: 1"); err == nil {
		t.Fatal("unknown param must be a config error (strict decoding)")
	}
}

func TestGroupOutOfRangeIsError(t *testing.T) {
	// Pattern has one capture group; asking for group 2 is invalid.
	if _, err := buildErr(t, "pattern: '(\\w+)'\ngroup: 2\nbaseline: b.txt\nmode: exact"); err == nil {
		t.Fatal("group index beyond the pattern's capture groups must be a config error")
	}
}

// TestWholeTreeInvariant pins the #4 mechanism: a baseline compares the whole
// scope's extracted set against a tracked file, so it must be evaluated
// whole-tree under --range rather than range-scoped (which would false-report
// removals or miss additions).
func TestWholeTreeInvariant(t *testing.T) {
	if !rules.IsWholeTreeInvariant(build(t, exactParams)) {
		t.Fatal("baseline must be a whole-tree invariant")
	}
}
