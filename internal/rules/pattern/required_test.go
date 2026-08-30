package pattern_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestRequiredRegexp2TimeoutFailsClosed: required-pattern is the other
// pcreMatcher consumer — a regexp2 match timeout must surface as an error
// (→ exit 2), not be swallowed as no-match (which in every-file mode would
// masquerade as a legitimately-missing pattern) (#22).
func TestRequiredRegexp2TimeoutFailsClosed(t *testing.T) {
	t.Parallel() // ~1s on the hardcoded 1s match timeout — overlap it with its siblings
	c := mustChecker(t, "required-pattern", "pattern: '(a+)+$'\nsyntax: regexp2\n")
	if _, err := c.CheckFile(scan.NewMemFile("a.txt", []byte(backtrackBomb+"\n"))); err == nil {
		t.Fatal("regexp2 match timeout must surface as an error, got nil (swallowed as no-match)")
	}
}

func TestRequiredEveryFileFlagsFilesMissingPattern(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'strict-flag'\n")
	ms, err := c.CheckFile(scan.NewMemFile("has.yaml", []byte("strict-flag: true\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("file with pattern flagged: %+v", ms)
	}
	ms, err = c.CheckFile(scan.NewMemFile("missing.yaml", []byte("other: 1\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 0 {
		t.Fatalf("missing pattern not flagged as file-level: %+v", ms)
	}
}

func TestRequiredExistsPassesWhenAnyFileMatches(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'anchor'\nmode: exists\n")
	if _, err := c.CheckFile(scan.NewMemFile("a.go", []byte("nothing\n"))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckFile(scan.NewMemFile("b.go", []byte("the anchor is here\n"))); err != nil {
		t.Fatal(err)
	}
	fin := c.(rules.Finalizer)
	if ms := fin.Finalize(); len(ms) != 0 {
		t.Fatalf("exists mode failed despite match: %+v", ms)
	}
}

func TestRequiredExistsFailsWhenNoFileMatches(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'anchor'\nmode: exists\n")
	if _, err := c.CheckFile(scan.NewMemFile("a.go", []byte("nothing\n"))); err != nil {
		t.Fatal(err)
	}
	fin := c.(rules.Finalizer)
	ms := fin.Finalize()
	if len(ms) != 1 || ms[0].Path != "" || ms[0].Line != 0 {
		t.Fatalf("want one scope-level match, got %+v", ms)
	}
}

func TestRequiredExistsZeroFilesSeenPasses(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'anchor'\nmode: exists\n")
	fin := c.(rules.Finalizer)
	if ms := fin.Finalize(); len(ms) != 0 {
		t.Fatalf("zero-file scope should pass at check time: %+v", ms)
	}
}

func TestRequiredRejectsUnknownMode(t *testing.T) {
	factory, _ := rules.Lookup("required-pattern")
	if _, err := factory(paramsNode(t, "pattern: x\nmode: sometimes\n")); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

// TestRequiredWholeTreeInvariantOnlyInExistsMode pins the #4 mechanism: exists
// mode is a whole-repo invariant (non-monotonic under file removal) and must be
// evaluated whole-tree under --range; every-file mode is per-file/monotonic and
// stays range-scopeable.
func TestRequiredWholeTreeInvariantOnlyInExistsMode(t *testing.T) {
	exists := mustChecker(t, "required-pattern", "pattern: 'anchor'\nmode: exists\n")
	if !rules.IsWholeTreeInvariant(exists) {
		t.Fatal("required-pattern in exists mode must be a whole-tree invariant")
	}
	everyFile := mustChecker(t, "required-pattern", "pattern: 'anchor'\nmode: every-file\n")
	if rules.IsWholeTreeInvariant(everyFile) {
		t.Fatal("required-pattern in every-file mode must stay range-scopeable")
	}
}
