package gotypes

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestMain makes this test binary the vettool when go vet runs it, as main
// does for the formwork binary, so the rule under test runs the analyzers it
// was built with.
func TestMain(m *testing.M) {
	if os.Getenv(VettoolEnv) == "1" {
		Main()
	}
	os.Exit(m.Run())
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadRule(t *testing.T, params string) rules.Checker {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(params), &node); err != nil {
		t.Fatal(err)
	}
	c, err := newForbiddenTypeRule(node.Content[0])
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// runRule hands the rule the given files, as the scan would, and finalizes it.
func runRule(t *testing.T, c rules.Checker, root string, given ...string) ([]rules.Match, error) {
	t.Helper()
	set, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, g := range given {
		want[g] = true
	}
	for _, f := range set.Files {
		if want[filepath.ToSlash(f.Path())] {
			if _, err := c.CheckFile(f); err != nil {
				t.Fatal(err)
			}
		}
	}
	return c.(rules.ErrFinalizer).FinalizeErr(rules.FinalizeContext{Root: root})
}

const anyParams = "types: [any]\npositions: [param, var]\n"

func TestRuleReportsOnlyTheFilesItWasGiven(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.26\n",
		"a/a.go":       "package a\n\nfunc F(x any) {}\n",
		"a/a_other.go": "package a\n\nfunc G(y any) {}\n",
		"b/b.go":       "package b\n\nfunc H() {\n\tvar z any\n\t_ = z\n}\n",
	})
	got, err := runRule(t, loadRule(t, anyParams), root, "a/a.go", "b/b.go")
	if err != nil {
		t.Fatal(err)
	}
	want := []rules.Match{
		{Path: "a/a.go", Line: 3, Message: "param is any, a type this rule forbids"},
		{Path: "b/b.go", Line: 4, Message: "var is any, a type this rule forbids"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %+v, want %+v", got, want)
	}
}

func TestRuleVetsEveryModuleItsFilesSitIn(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":           "module example.com/top\n\ngo 1.26\n",
		"top.go":           "package top\n\nfunc F(x any) {}\n",
		"inner/go.mod":     "module example.com/inner\n\ngo 1.26\n",
		"inner/deep/in.go": "package deep\n\nfunc G(y any) {}\n",
	})
	got, err := runRule(t, loadRule(t, anyParams), root, "top.go", "inner/deep/in.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "inner/deep/in.go" || got[1].Path != "top.go" {
		t.Fatalf("matches = %+v, want one in each module", got)
	}
}

func TestRuleFailsClosedWhereGoVetCannotAnswer(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"type error": {
			"go.mod": "module example.com/m\n\ngo 1.26\n",
			"a/a.go": "package a\n\nfunc F() int { return \"s\" }\n",
		},
		// The only go.mod is above the root, where the rule must not reach:
		// go vet would happily type-check the file as part of that module.
		"go.mod only above the root": {
			"go.mod":      "module example.com/outer\n\ngo 1.26\n",
			"root/a/a.go": "package a\n\nfunc F(x any) {}\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := writeTree(t, files)
			if _, err := os.Stat(filepath.Join(root, "root")); err == nil {
				root = filepath.Join(root, "root")
			}
			got, err := runRule(t, loadRule(t, anyParams), root, "a/a.go")
			if err == nil {
				t.Fatalf("no error, matches %+v", got)
			}
		})
	}
}

func TestRuleWithNoFilesRunsNothing(t *testing.T) {
	prev := vettool
	vettool = func() (string, error) { t.Fatal("go vet ran with no files"); return "", nil }
	t.Cleanup(func() { vettool = prev })
	got, err := runRule(t, loadRule(t, anyParams), t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("matches %+v err %v", got, err)
	}
}

func TestRuleRefusesParamsTheVettoolWouldRefuse(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("types: [any]\npositions: [lhs]\n"), &node); err != nil {
		t.Fatal(err)
	}
	if _, err := newForbiddenTypeRule(node.Content[0]); err == nil || !strings.Contains(err.Error(), "lhs") {
		t.Fatalf("err = %v, want the unknown position named", err)
	}
}

func TestAnAnalyzerErrorInTheReportFailsTheRule(t *testing.T) {
	_, err := parseVetJSON("/r", strings.NewReader(`{"p":{"formwork":{"error":"broke"}}}`))
	if err == nil || !strings.Contains(err.Error(), "broke") {
		t.Fatalf("err = %v", err)
	}
}

// formwork check without -C evaluates the tree at ".", and every position
// go vet prints is absolute.
func TestRuleTakesARelativeRoot(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.26\n",
		"a.go":   "package m\n\nfunc F(x any) {}\n",
	})
	t.Chdir(root)
	got, err := runRule(t, loadRule(t, anyParams), ".", "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Fatalf("matches = %+v, want one in a.go", got)
	}
}
