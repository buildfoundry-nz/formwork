package scan_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkReturnsSortedRelativePathsSkippingGitAndFormwork(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "b.go"), "package b\n")
	writeFile(t, filepath.Join(root, "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, "README.md"), "# hi\n")
	writeFile(t, filepath.Join(root, ".git", "config"), "x\n")
	writeFile(t, filepath.Join(root, ".formwork", "formwork.yaml"), "version: 1\n")

	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fset.Files {
		got = append(got, f.Path())
	}
	want := []string{"README.md", "src/a.go", "src/b.go"}
	if len(got) != len(want) {
		t.Fatalf("paths: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths: got %v want %v", got, want)
		}
	}
}

func TestContentIsCachedAfterFirstRead(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.txt")
	writeFile(t, p, "original")
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := fset.Files[0].Content()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, "changed on disk")
	second, err := fset.Files[0].Content()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != "original" || string(second) != "original" {
		t.Fatalf("content not cached: first=%q second=%q", first, second)
	}
}

func TestLinesSplitsWithoutTrailingEmptyLine(t *testing.T) {
	f := scan.NewMemFile("x.txt", []byte("one\ntwo\n"))
	lines, err := f.Lines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "two" {
		t.Fatalf("lines: %q", lines)
	}
	empty := scan.NewMemFile("e.txt", nil)
	lines, err = empty.Lines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("empty file lines: %q", lines)
	}
}

// TestWalkRefusesSourceExtensionSymlink: a committed symlink whose name looks
// like source (e.g. helper.go → helper.txt) is invisible to every rule today
// while the Go toolchain follows it and compiles the target. Walk must refuse
// that shape loudly (spec §11: never skip silently) rather than silently
// skip non-regular files. Non-source symlinks stay skipped — see
// TestWalkSkipsNonSourceSymlinks.
func TestWalkRefusesSourceExtensionSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "helper.txt"), "package helper\nconst Token = 1\n")
	link := filepath.Join(root, "helper.go")
	if err := os.Symlink("helper.txt", link); err != nil {
		t.Fatal(err)
	}
	_, err := scan.Walk(root)
	if err == nil {
		t.Fatal("expected Walk to refuse a .go symlink; got nil error (silent skip is the bug)")
	}
	if !strings.Contains(err.Error(), "helper.go") {
		t.Fatalf("error should name the symlink path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error should identify the problem as a symlink, got: %v", err)
	}
}

// TestWalkSkipsNonSourceSymlinks: config/docs symlinks (e.g. formwork.yaml →
// .formwork/formwork.yaml) are not a Go/Dart compile bypass. They remain
// un-scanned but do not fail the walk — only source-extension names are loud.
func TestWalkSkipsNonSourceSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(root, "src", "a.go"), "package a\n")
	if err := os.Symlink("real.yaml", filepath.Join(root, "alias.yaml")); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatalf("non-source symlink must not fail Walk: %v", err)
	}
	var got []string
	for _, f := range fset.Files {
		got = append(got, f.Path())
	}
	// alias.yaml is a symlink → not in the FileSet; real.yaml and src/a.go are.
	want := map[string]bool{"real.yaml": true, "src/a.go": true}
	if len(got) != 2 {
		t.Fatalf("paths: got %v", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected path %q in %v", p, got)
		}
	}
}

// TestUnderBuiltinSkip pins the helper the scan-ignore-tracked fallback (#90)
// uses to re-establish the built-in-skip exclusion for paths the walk never
// saw: it must agree with the walk's own prune — .git at any depth, .formwork
// at the walk root only (#268) — so a record-free tracked path the walk really
// never enumerates can never be reported as scan.ignore-hidden, and one it
// does enumerate is not excused from that check.
func TestUnderBuiltinSkip(t *testing.T) {
	for path, want := range map[string]bool{
		".formwork/rules/r.yaml":     true,
		".git/config":                true,
		"a/b/.git/hooks/pre-commit":  true,
		"a/.formwork/x":              false, // a nested project's own config: scanned
		"src/main.go":                false,
		"formwork/rules/r.yaml":      false, // no dot — not the skip name
		"a/.github/workflows/ci.yml": false,
	} {
		if got := scan.UnderBuiltinSkip(path); got != want {
			t.Errorf("UnderBuiltinSkip(%q) = %v, want %v", path, got, want)
		}
	}
}
