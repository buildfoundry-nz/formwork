package pattern_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// "A file that calls _confirmDeactivate must define it with the messenger
// captured before the await": a requirement scoped by when, with a multi-line
// shape.
func TestRequiredWhenMultiline(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'Future<void> _confirm\\(\\)[\\s\\S]*?messenger[\\s\\S]*?await'\n"+
		"when: '_confirm\\('\nmultiline: true\n")
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"not applicable", "void other() {}\n", 0},
		{"applies and holds", "Future<void> _confirm() {\n  final messenger = m();\n  await x();\n}\n", 0},
		{"applies and missing", "Future<void> _confirm() {\n  await x();\n  final messenger = m();\n}\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms, err := c.CheckFile(scan.NewMemFile("a.dart", []byte(tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			if len(ms) != tc.want {
				t.Fatalf("want %d findings, got %+v", tc.want, ms)
			}
		})
	}
}

func TestRequiredWhenRefusedInExistsMode(t *testing.T) {
	if _, err := buildChecker("required-pattern", "pattern: x\nmode: exists\nwhen: y\n"); err == nil {
		t.Fatal("when must be refused in exists mode")
	}
}

// A list is a conjunction: the requirement applies only where every entry
// matches.
func TestRequiredWhenListIsAConjunction(t *testing.T) {
	c := mustChecker(t, "required-pattern", "pattern: 'FakeRepo'\nwhen: ['wakes the rail', 'FakeRail']\n")
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"one of two", "// wakes the rail\n", 0},
		{"both, and holds", "// wakes the rail\nFakeRail()\nFakeRepo()\n", 0},
		{"both, missing", "// wakes the rail\nFakeRail()\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms, err := c.CheckFile(scan.NewMemFile("a_test.dart", []byte(tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			if len(ms) != tc.want {
				t.Fatalf("want %d findings, got %+v", tc.want, ms)
			}
		})
	}
}
