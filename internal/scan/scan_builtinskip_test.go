package scan_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// UnderBuiltinSkipDir and UnderBuiltinSkip differ on exactly one input shape,
// and that difference was a fail-open in a caller (#158): the walk consults its
// skip set only for directories, so a regular file NAMED .formwork is scanned
// and enforced on. A predicate that calls it "under a built-in skip"
// contradicts the walk's own verdict.
func TestUnderBuiltinSkipDirExcludesTheLeaf(t *testing.T) {
	for _, tc := range []struct {
		path     string
		underDir bool
		underAny bool
	}{
		{"src/a.go", false, false},
		{".formwork/rules/r.yaml", true, true},
		{"a/.git/config", true, true},
		// The leaf cases: a FILE with a skip-directory's name.
		{".formwork", false, true},
		{"sub/.git", false, true},
		// A skip ancestor still wins over a skip-named leaf.
		{".formwork/.formwork", true, true},
		// A NESTED .formwork is not in the set at all since #268 — neither as
		// a leaf nor as an ancestor — so this row now says false twice for a
		// reason that has nothing to do with the leaf distinction above.
		{"sub/.formwork", false, false},
		{"sub/.formwork/rules/r.yaml", false, false},
	} {
		if got := scan.UnderBuiltinSkipDir(tc.path); got != tc.underDir {
			t.Errorf("UnderBuiltinSkipDir(%q) = %v, want %v", tc.path, got, tc.underDir)
		}
		if got := scan.UnderBuiltinSkip(tc.path); got != tc.underAny {
			t.Errorf("UnderBuiltinSkip(%q) = %v, want %v", tc.path, got, tc.underAny)
		}
	}
}
