package pattern_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// The derived gate sits in front of both scan modes. A file that holds the
// literal must still be scanned and reported; one that lacks it reports
// nothing either way, so only the first half can catch a gate wired wrong.
func TestForbiddenAutoGateKeepsEveryMatch(t *testing.T) {
	for _, mode := range []string{"", "multiline: true\n"} {
		c := mustChecker(t, "forbidden-pattern", "pattern: 'json\\.Marshal\\('\n"+mode)
		ms, err := c.CheckFile(scan.NewMemFile("a.go", []byte("package a\n\nvar b, _ = json.Marshal(v)\n")))
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != 1 || ms[0].Line != 3 {
			t.Errorf("mode %q: matches = %+v, want one at line 3: the gate must not hide a file that holds its literal", mode, ms)
		}
		ms, err = c.CheckFile(scan.NewMemFile("b.go", []byte("package b\n")))
		if err != nil || len(ms) != 0 {
			t.Errorf("mode %q: a file without the literal = %+v, %v; want no match", mode, ms, err)
		}
	}
}
