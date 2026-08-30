package gitdiff_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	"gopkg.in/yaml.v3"
)

func build(t *testing.T, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup("git-diff")
	if !ok {
		t.Fatal("git-diff not registered")
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
		t.Fatalf("build git-diff %q: %v", params, err)
	}
	return c
}

func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "T"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	return dir
}

func git(t *testing.T, dir string, a ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", a, err, out)
	}
}

func write(t *testing.T, dir, rel, s string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fin(t *testing.T, c rules.Checker, root string) ([]rules.Match, error) {
	t.Helper()
	return c.(rules.ErrFinalizer).FinalizeErr(rules.FinalizeContext{Root: root})
}

func TestGitDiffForbidAddedFires(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "a.go", "package a\nvar x = pgxpool.New()\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "adds forbidden")

	c := build(t, "range: HEAD~1..HEAD\nforbid_added: 'pgxpool\\.New'")
	m, err := fin(t, c, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || !strings.Contains(m[0].Message, "added") {
		t.Fatalf("expected one forbidden-added finding, got %v", m)
	}
}

func TestGitDiffForbidRemovedFires(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a\n// LICENSE-HEADER\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "a.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "removes header")

	c := build(t, "range: HEAD~1..HEAD\nforbid_removed: LICENSE-HEADER")
	m, err := fin(t, c, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || !strings.Contains(m[0].Message, "removed") {
		t.Fatalf("expected one forbidden-removed finding, got %v", m)
	}
}

func TestGitDiffCleanRangePasses(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.go", "package a\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	write(t, dir, "a.go", "package a\nvar ok = 1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "innocuous")

	c := build(t, "range: HEAD~1..HEAD\nforbid_added: 'pgxpool\\.New'")
	m, err := fin(t, c, dir)
	if err != nil || len(m) != 0 {
		t.Fatalf("clean range should pass, got matches=%v err=%v", m, err)
	}
}

func TestGitDiffGitFailureIsEngineError(t *testing.T) {
	dir := t.TempDir() // not a git repo
	c := build(t, "range: HEAD~1..HEAD\nforbid_added: x")
	if _, err := fin(t, c, dir); err == nil {
		t.Fatal("git failure must be an engine error, not a pass")
	}
}

func TestGitDiffValidation(t *testing.T) {
	f, _ := rules.Lookup("git-diff")
	// missing range
	if _, err := f(mustNode(t, "forbid_added: x")); err == nil {
		t.Error("missing range should error")
	}
	// no forbid clause
	if _, err := f(mustNode(t, "range: A..B")); err == nil {
		t.Error("missing forbid clause should error")
	}
	if c := build(t, "range: A..B\nforbid_added: x"); rules.CostOf(c) != rules.CostHeavy {
		t.Error("git-diff must be heavy")
	} else if rules.ProcessBoundOf(c) {
		t.Error("git-diff is CostHeavy for --skip-escapes, not an analyzer-class process")
	}
}

func mustNode(t *testing.T, s string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}
