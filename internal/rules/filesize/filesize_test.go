package filesize_test

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

func mustChecker(t *testing.T, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup("file-size")
	if !ok {
		t.Fatal("type \"file-size\" not registered")
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	return c
}

// fileOf returns an in-memory file at path with n lines of content.
func fileOf(path string, n int) *scan.File {
	return scan.NewMemFile(path, []byte(strings.Repeat("x\n", n)))
}

func check(t *testing.T, c rules.Checker, f *scan.File) []rules.Match {
	t.Helper()
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	return ms
}

func TestFileSizeUnderDefaultCapPasses(t *testing.T) {
	c := mustChecker(t, "cap: 3\n")
	if ms := check(t, c, fileOf("a.go", 3)); len(ms) != 0 {
		t.Fatalf("file at cap flagged: %+v", ms)
	}
	if ms := check(t, c, fileOf("b.go", 1)); len(ms) != 0 {
		t.Fatalf("small file flagged: %+v", ms)
	}
}

func TestFileSizeOverDefaultCapFlags(t *testing.T) {
	c := mustChecker(t, "cap: 3\n")
	ms := check(t, c, fileOf("big.go", 5))
	if len(ms) != 1 {
		t.Fatalf("over-cap file not flagged once: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "5") || !strings.Contains(ms[0].Message, "3") {
		t.Fatalf("message should state lines vs cap: %q", ms[0].Message)
	}
	if ms[0].Path != "" {
		t.Fatalf("path should be left for the engine to fill: %q", ms[0].Path)
	}
}

func TestFileSizeDefaultCapZeroIsUnlimited(t *testing.T) {
	// cap 0 = no default cap; only overrides/hard_cap constrain.
	c := mustChecker(t, "cap: 0\noverrides:\n  - glob: \"*.md\"\n    cap: 2\n")
	if ms := check(t, c, fileOf("code.go", 9999)); len(ms) != 0 {
		t.Fatalf("unlimited default flagged a file: %+v", ms)
	}
}

func TestFileSizeOverrideRaisesCap(t *testing.T) {
	c := mustChecker(t, "cap: 3\noverrides:\n  - glob: \"gen/**\"\n    cap: 10\n")
	if ms := check(t, c, fileOf("gen/big.go", 8)); len(ms) != 0 {
		t.Fatalf("override should have raised the cap to 10: %+v", ms)
	}
	if ms := check(t, c, fileOf("hand/big.go", 8)); len(ms) != 1 {
		t.Fatalf("non-override file should use default cap 3: %+v", ms)
	}
}

func TestFileSizeOverrideLowersCap(t *testing.T) {
	c := mustChecker(t, "cap: 10\noverrides:\n  - glob: \"*.md\"\n    cap: 2\n")
	if ms := check(t, c, fileOf("README.md", 5)); len(ms) != 1 {
		t.Fatalf("override should have lowered the cap to 2: %+v", ms)
	}
	if ms := check(t, c, fileOf("main.go", 5)); len(ms) != 0 {
		t.Fatalf("non-override file should use default cap 10: %+v", ms)
	}
}

func TestFileSizeFirstMatchingOverrideWins(t *testing.T) {
	c := mustChecker(t,
		"cap: 100\noverrides:\n  - glob: \"src/**\"\n    cap: 2\n  - glob: \"src/gen/**\"\n    cap: 100\n")
	// src/gen/x.go matches both globs; the first (cap 2) must win.
	if ms := check(t, c, fileOf("src/gen/x.go", 50)); len(ms) != 1 {
		t.Fatalf("first matching override (cap 2) should win: %+v", ms)
	}
}

func TestFileSizeOverrideCapZeroIsUnlimited(t *testing.T) {
	c := mustChecker(t, "cap: 3\noverrides:\n  - glob: \"vendor/**\"\n    cap: 0\n")
	if ms := check(t, c, fileOf("vendor/huge.go", 500)); len(ms) != 0 {
		t.Fatalf("override cap 0 should be unlimited: %+v", ms)
	}
}

func TestFileSizeHardCapEnforcedOverOverride(t *testing.T) {
	c := mustChecker(t,
		"cap: 3\nhard_cap: 10\noverrides:\n  - glob: \"gen/**\"\n    cap: 1000\n")
	// Override raises the cap to 1000, but the hard cap of 10 is an absolute ceiling.
	ms := check(t, c, fileOf("gen/x.go", 20))
	if len(ms) != 1 {
		t.Fatalf("hard cap should bind despite the override: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "10") {
		t.Fatalf("message should cite the hard cap of 10: %q", ms[0].Message)
	}
	// Under the hard cap the override still allows a large file past the default.
	if ms := check(t, c, fileOf("gen/y.go", 8)); len(ms) != 0 {
		t.Fatalf("file under hard cap and override should pass: %+v", ms)
	}
}

func TestFileSizeHardCapWithUnlimitedDefault(t *testing.T) {
	c := mustChecker(t, "hard_cap: 4\n")
	if ms := check(t, c, fileOf("a.go", 4)); len(ms) != 0 {
		t.Fatalf("file at hard cap flagged: %+v", ms)
	}
	if ms := check(t, c, fileOf("b.go", 5)); len(ms) != 1 {
		t.Fatalf("file over hard cap not flagged: %+v", ms)
	}
}

func TestFileSizeRejectsBadParams(t *testing.T) {
	factory, ok := rules.Lookup("file-size")
	if !ok {
		t.Fatal("type \"file-size\" not registered")
	}
	cases := map[string]string{
		"nothing configured":    "{}\n",
		"negative cap":          "cap: -1\n",
		"negative hard_cap":     "hard_cap: -1\n",
		"unknown field":         "cap: 3\ncapp: 4\n",
		"empty override glob":   "cap: 3\noverrides:\n  - glob: \"\"\n    cap: 2\n",
		"invalid override glob": "cap: 3\noverrides:\n  - glob: \"[\"\n    cap: 2\n",
		"negative override cap": "cap: 3\noverrides:\n  - glob: \"a/**\"\n    cap: -2\n",
	}
	for name, src := range cases {
		if _, err := factory(paramsNode(t, src)); err == nil {
			t.Errorf("%s: expected a config error, got nil", name)
		}
	}
	// A wholly empty params node (nil) is also nothing-configured.
	if _, err := factory(nil); err == nil {
		t.Error("nil params: expected a config error, got nil")
	}
}
