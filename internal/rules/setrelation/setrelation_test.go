package setrelation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"

	// Pull every preprocess init so side preprocess: decomment-go is resolvable.
	_ "github.com/buildfoundry-nz/formwork/internal/preprocess"
)

func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup("set-relation")
	if !ok {
		t.Fatal("set-relation type not registered")
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
		t.Fatalf("build set-relation %q: %v", params, err)
	}
	return c
}

func buildErr(t *testing.T, params string) error {
	t.Helper()
	f, ok := rules.Lookup("set-relation")
	if !ok {
		t.Fatal("set-relation type not registered")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
		t.Fatal(err)
	}
	var node *yaml.Node
	if len(doc.Content) > 0 {
		node = doc.Content[0]
	}
	_, err := f(node)
	return err
}

func feed(t *testing.T, c rules.Checker, path, content string) {
	t.Helper()
	if _, err := c.CheckFile(scan.NewMemFile(path, []byte(content))); err != nil {
		t.Fatalf("CheckFile(%q): %v", path, err)
	}
}

func finalize(t *testing.T, c rules.Checker) []rules.Match {
	t.Helper()
	fin, ok := c.(rules.Finalizer)
	if !ok {
		t.Fatal("set-relation should implement Finalizer")
	}
	return fin.Finalize()
}

const subsetParams = "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)'}\n" +
	"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n" +
	"relation: subset\n"

func TestSubsetPasses(t *testing.T) {
	c := build(t, subsetParams)
	feed(t, c, "a/1.txt", "id=foo\nid=bar\n")
	feed(t, c, "b/1.txt", "id=foo\nid=bar\nid=baz\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("A subset of B should pass, got %+v", ms)
	}
}

func TestSubsetFailsAndNamesOffenders(t *testing.T) {
	c := build(t, subsetParams)
	feed(t, c, "a/1.txt", "id=foo\nid=zzz\n")
	feed(t, c, "b/1.txt", "id=foo\n")
	ms := finalize(t, c)
	if len(ms) != 1 || ms[0].Path != "" || ms[0].Line != 0 {
		t.Fatalf("want one scope-level match, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "zzz") {
		t.Fatalf("message should name offending element, got %q", ms[0].Message)
	}
	if strings.Contains(ms[0].Message, "foo") {
		t.Fatalf("message should not name in-B element, got %q", ms[0].Message)
	}
}

func TestEqualPasses(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"relation: equal\n")
	feed(t, c, "a/1.txt", "id=foo\nid=bar\n")
	feed(t, c, "b/1.txt", "id=bar\nid=foo\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("equal sets should pass, got %+v", ms)
	}
}

func TestEqualFailsReportsBothSides(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"relation: equal\n")
	feed(t, c, "a/1.txt", "id=foo\nid=onlya\n")
	feed(t, c, "b/1.txt", "id=foo\nid=onlyb\n")
	ms := finalize(t, c)
	if len(ms) != 1 {
		t.Fatalf("want one match, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "onlya") || !strings.Contains(ms[0].Message, "onlyb") {
		t.Fatalf("message should name both divergent elements, got %q", ms[0].Message)
	}
}

func TestDisjointPasses(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"relation: disjoint\n")
	feed(t, c, "a/1.txt", "id=foo\n")
	feed(t, c, "b/1.txt", "id=bar\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("disjoint sets should pass, got %+v", ms)
	}
}

func TestDisjointFailsNamesIntersection(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"relation: disjoint\n")
	feed(t, c, "a/1.txt", "id=foo\nid=shared\n")
	feed(t, c, "b/1.txt", "id=shared\nid=bar\n")
	ms := finalize(t, c)
	if len(ms) != 1 {
		t.Fatalf("want one match, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "shared") {
		t.Fatalf("message should name intersecting element, got %q", ms[0].Message)
	}
	if strings.Contains(ms[0].Message, "foo") || strings.Contains(ms[0].Message, "bar") {
		t.Fatalf("message should name only the intersection, got %q", ms[0].Message)
	}
}

func TestOffendersAreSortedDeterministically(t *testing.T) {
	c := build(t, subsetParams)
	// A has c, a, b (none in empty B); message must list them sorted.
	feed(t, c, "a/1.txt", "id=c\nid=a\nid=b\n")
	ms := finalize(t, c)
	if len(ms) != 1 {
		t.Fatalf("want one match, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "a, b, c") {
		t.Fatalf("offenders must be sorted, got %q", ms[0].Message)
	}
}

func TestCustomGroupSelectsCaptureIndex(t *testing.T) {
	// group 2 selects the second capture group.
	c := build(t, "a: {files: ['a/*.txt'], pattern: '(\\w+):(\\w+)', group: 2}\n"+
		"b: {files: ['b/*.txt'], pattern: '(\\w+):(\\w+)', group: 2}\n"+
		"relation: equal\n")
	feed(t, c, "a/1.txt", "key:val\n")
	feed(t, c, "b/1.txt", "other:val\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("group 2 (val==val) should be equal, got %+v", ms)
	}
}

func TestZeroFilesPasses(t *testing.T) {
	c := build(t, subsetParams)
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("empty scope should pass at finalize, got %+v", ms)
	}
}

func TestRejectsBadParams(t *testing.T) {
	cases := map[string]string{
		"missing a":         "b: {files: ['b/*.txt'], pattern: 'x'}\nrelation: subset\n",
		"missing a.files":   "a: {pattern: 'x'}\nb: {files: ['b/*'], pattern: 'x'}\nrelation: subset\n",
		"missing a.pattern": "a: {files: ['a/*']}\nb: {files: ['b/*'], pattern: 'x'}\nrelation: subset\n",
		"missing relation":  "a: {files: ['a/*'], pattern: 'x'}\nb: {files: ['b/*'], pattern: 'x'}\n",
		"unknown relation":  "a: {files: ['a/*'], pattern: 'x'}\nb: {files: ['b/*'], pattern: 'x'}\nrelation: overlaps\n",
		"invalid glob":      "a: {files: ['a/['], pattern: 'x'}\nb: {files: ['b/*'], pattern: 'x'}\nrelation: subset\n",
		"invalid regex":     "a: {files: ['a/*'], pattern: '('}\nb: {files: ['b/*'], pattern: 'x'}\nrelation: subset\n",
		"group out of range": "a: {files: ['a/*'], pattern: 'id=(\\w+)', group: 5}\n" +
			"b: {files: ['b/*'], pattern: 'id=(\\w+)'}\nrelation: subset\n",
		"unknown field": "a: {files: ['a/*'], pattern: 'x'}\nb: {files: ['b/*'], pattern: 'x'}\nrelation: subset\nnope: 1\n",
	}
	for name, params := range cases {
		if err := buildErr(t, params); err == nil {
			t.Errorf("%s: expected config error, got nil", name)
		}
	}
}

func TestFileMatchingNeitherGroupIgnored(t *testing.T) {
	c := build(t, subsetParams)
	feed(t, c, "c/other.txt", "id=ignored\n") // matches neither a/ nor b/
	feed(t, c, "b/1.txt", "id=foo\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("out-of-scope file must not contribute to A, got %+v", ms)
	}
}

// TestWholeTreeInvariant pins the #4 mechanism: a set-relation joins two
// file-derived sets across the whole scope, so it must be evaluated whole-tree
// under --range rather than range-scoped (which would drop elements and
// false-report the relation).
func TestWholeTreeInvariant(t *testing.T) {
	if !rules.IsWholeTreeInvariant(build(t, subsetParams)) {
		t.Fatal("set-relation must be a whole-tree invariant")
	}
}

// TestMinCountEmptyEqualFails pins vacuity audit V4: empty∩empty / empty=empty
// is green by set algebra when neither side declares min_count, so a rule that
// claims "A equals B" over test evidence can pass on zero extracted elements.
// min_count ≥ 1 on either side makes that unrepresentable.
func TestMinCountEmptyEqualFails(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)', min_count: 1}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)', min_count: 1}\n"+
		"relation: equal\n")
	// No files fed → both sides empty → must fail min_count, not green equal.
	ms := finalize(t, c)
	if len(ms) == 0 {
		t.Fatal("empty∩empty with min_count≥1 must FAIL, not pass as equal")
	}
	if !strings.Contains(ms[0].Message, "min_count") {
		t.Fatalf("finding must name min_count, got %q", ms[0].Message)
	}
}

// TestMinCountEmptySubsetFails is the subset twin: ∅ ⊆ B is true in set algebra
// even when A extracted nothing, so a "every X has a test" claim is vacuous
// unless A.min_count ≥ 1 refuses the empty left side.
func TestMinCountEmptySubsetFails(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)', min_count: 1}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"relation: subset\n")
	feed(t, c, "b/1.txt", "id=foo\n")
	ms := finalize(t, c)
	if len(ms) == 0 {
		t.Fatal("empty A with min_count≥1 must FAIL subset, not pass as ∅ ⊆ B")
	}
	if !strings.Contains(ms[0].Message, "min_count") && !strings.Contains(ms[0].Message, "side A") {
		t.Fatalf("finding must name the short side, got %q", ms[0].Message)
	}
}

// TestMinCountSatisfiedPasses: min_count is a floor, not a relation substitute.
// Equal sets of size ≥ min still pass.
func TestMinCountSatisfiedPasses(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)', min_count: 1}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)', min_count: 1}\n"+
		"relation: equal\n")
	feed(t, c, "a/1.txt", "id=foo\n")
	feed(t, c, "b/1.txt", "id=foo\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("equal sets meeting min_count must pass, got %+v", ms)
	}
}

// TestMinCountDefaultZeroKeepsBackCompat: omitting min_count preserves today's
// empty-scope green for subset/equal/disjoint so existing rules do not flip red.
func TestMinCountDefaultZeroKeepsBackCompat(t *testing.T) {
	c := build(t, "a: {files: ['a/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"b: {files: ['b/*.txt'], pattern: 'id=(\\w+)'}\n"+
		"relation: equal\n")
	if ms := finalize(t, c); len(ms) != 0 {
		t.Fatalf("default min_count=0 must keep empty equal green, got %+v", ms)
	}
}

// TestMinCountRejectsNegative pins config-time rejection of a nonsense floor.
func TestMinCountRejectsNegative(t *testing.T) {
	err := buildErr(t, "a: {files: ['a/*'], pattern: 'id=(\\w+)', min_count: -1}\n"+
		"b: {files: ['b/*'], pattern: 'id=(\\w+)'}\nrelation: equal\n")
	if err == nil {
		t.Fatal("negative min_count accepted")
	}
	if !strings.Contains(err.Error(), "min_count") {
		t.Fatalf("error must name min_count, got %v", err)
	}
}

// TestLinesErrorIsNotSilentEmpty pins I16: a Lines()/Content() failure on a
// matching side file must surface as a CheckFile error (engine exit 2), never
// as an empty set that makes subset vacuously green.
func TestLinesErrorIsNotSilentEmpty(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "a")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aPath := filepath.Join(aDir, "1.txt")
	if err := os.WriteFile(aPath, []byte("id=foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bDir := filepath.Join(root, "b")
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bDir, "1.txt"), []byte("id=foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	// Remove A after the walk so the File still points at the path but Content
	// fails — the I/O error class that used to be swallowed into an empty set.
	if err := os.Remove(aPath); err != nil {
		t.Fatal(err)
	}
	c := build(t, subsetParams)
	var sawErr error
	for _, f := range fset.Files {
		if _, err := c.CheckFile(f); err != nil {
			sawErr = err
			break
		}
	}
	if sawErr == nil {
		t.Fatal("Lines()/Content() failure on an A-side file must be a CheckFile error, not a silent empty set")
	}
}

// TestSidePreprocessAppliesPerSide pins per-side preprocess: B can strip
// comments while A does not, so a name that lives only in a B-side comment is
// not a join element once B declares decomment-go.
func TestSidePreprocessAppliesPerSide(t *testing.T) {
	c := build(t, "a: {files: ['a/*.go'], pattern: 'id=(\\w+)'}\n"+
		"b: {files: ['b/*.go'], pattern: 'id=(\\w+)', preprocess: decomment-go}\n"+
		"relation: equal\n")
	feed(t, c, "a/1.go", "package a\nid=foo\n")
	// foo only appears in a B comment — decomment-go must drop it so equal fails.
	feed(t, c, "b/1.go", "package b\n// id=foo\n")
	ms := finalize(t, c)
	if len(ms) == 0 {
		t.Fatal("B-side decomment-go must drop the comment-only element so equal fails")
	}
}

// TestSidePreprocessRejectsUnknown names the bad transform at config time.
func TestSidePreprocessRejectsUnknown(t *testing.T) {
	err := buildErr(t, "a: {files: ['a/*'], pattern: 'id=(\\w+)', preprocess: not-a-real-transform}\n"+
		"b: {files: ['b/*'], pattern: 'id=(\\w+)'}\nrelation: equal\n")
	if err == nil {
		t.Fatal("unknown side preprocess accepted")
	}
	if !strings.Contains(err.Error(), "preprocess") {
		t.Fatalf("error must name preprocess, got %v", err)
	}
}
