package scan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

func paths(fs *scan.FileSet) []string {
	var out []string
	for _, f := range fs.Files {
		out = append(out, f.Path())
	}
	return out
}

func TestWalkIgnoringSkipsMatchingFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "keep.go"), "package a\n")
	writeFile(t, filepath.Join(root, "src", "drop.gen.go"), "package a\n")
	fs, err := scan.WalkIgnoring(root, []string{"**/*.gen.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"src/keep.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	want := []scan.Ignored{{Path: "src/drop.gen.go", Dir: false, Glob: "**/*.gen.go"}}
	if !reflect.DeepEqual(fs.Ignored, want) {
		t.Fatalf("Ignored = %#v, want %#v", fs.Ignored, want)
	}
}

func TestWalkIgnoringPrunesDirViaTrailingDoublestar(t *testing.T) {
	// Pins the doublestar zero-segment contract this design depends on:
	// Match("p/**", "p") == true, so "p/**" prunes dir p without descending.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "worktrees", "wt", "dup.go"), "package a\n")
	writeFile(t, filepath.Join(root, "main.go"), "package a\n")
	fs, err := scan.WalkIgnoring(root, []string{".claude/worktrees/**"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"main.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	// One Dir record for the pruned root; nothing below it is enumerated.
	want := []scan.Ignored{{Path: ".claude/worktrees", Dir: true, Glob: ".claude/worktrees/**"}}
	if !reflect.DeepEqual(fs.Ignored, want) {
		t.Fatalf("Ignored = %#v, want %#v", fs.Ignored, want)
	}
}

func TestWalkIgnoringPrunesDirNamedAtAnyDepth(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "build", "out.go"), "package a\n")
	writeFile(t, filepath.Join(root, "b", "build", "out.go"), "package b\n")
	writeFile(t, filepath.Join(root, "b", "src.go"), "package b\n")
	fs, err := scan.WalkIgnoring(root, []string{"**/build"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"b/src.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if len(fs.Ignored) != 2 || !fs.Ignored[0].Dir || !fs.Ignored[1].Dir {
		t.Fatalf("Ignored = %#v, want two Dir records", fs.Ignored)
	}
}

func TestWalkIgnoringSourceSymlinkInsideIgnoredTreeDoesNotError(t *testing.T) {
	// #54's refusal is load-bearing OUTSIDE ignored trees; inside one, the
	// operator has declared the subtree not-ours, so the walk must not die on it.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "vendor", "real.go"), "package v\n")
	if err := os.Symlink("real.go", filepath.Join(root, "vendor", "alias.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "main.go"), "package a\n")
	fs, err := scan.WalkIgnoring(root, []string{"vendor/**"})
	if err != nil {
		t.Fatalf("walk errored on symlink inside ignored tree: %v", err)
	}
	if got, want := paths(fs), []string{"main.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

func TestWalkIgnoringSourceSymlinkOutsideIgnoreStillRefused(t *testing.T) {
	// Regression guard: adding ignore support must not weaken #54 anywhere else.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.go"), "package a\n")
	if err := os.Symlink("real.go", filepath.Join(root, "alias.go")); err != nil {
		t.Fatal(err)
	}
	_, err := scan.WalkIgnoring(root, []string{"vendor/**"})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v, want symlink refusal", err)
	}
}

func TestWalkEqualsWalkIgnoringNil(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, ".git", "config"), "x\n")
	a, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := scan.WalkIgnoring(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths(a), paths(b)) || len(b.Ignored) != 0 {
		t.Fatalf("Walk %v vs WalkIgnoring(nil) %v (Ignored %v)", paths(a), paths(b), b.Ignored)
	}
}

func TestWalkIgnoringAttributesToFirstMatchingGlob(t *testing.T) {
	// Overlapping entries: the FIRST matching glob (config order) claims every
	// record, so a later, fully-shadowed entry renders as "0 matches" in the
	// lint census — indistinguishable from a typo, and safe to delete since it
	// removes nothing. Pinned so attribution is a contract, not an accident.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "vendor", "sub", "x.go"), "package v\n")
	fs, err := scan.WalkIgnoring(root, []string{"vendor/**", "vendor/sub/**"})
	if err != nil {
		t.Fatal(err)
	}
	want := []scan.Ignored{{Path: "vendor", Dir: true, Glob: "vendor/**"}}
	if !reflect.DeepEqual(fs.Ignored, want) {
		t.Fatalf("Ignored = %#v, want first-glob attribution %#v", fs.Ignored, want)
	}
}

func TestWalkIgnoringOneGlobCanRecordBothDirAndFile(t *testing.T) {
	// A file-shaped glob can match a directory's NAME (pruning its subtree —
	// the disclosed widening) and a real file elsewhere in the same run; the
	// two record kinds coexist and sort by path.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "util.gen.go", "notes.txt"), "x\n")
	writeFile(t, filepath.Join(root, "b", "y.gen.go"), "package b\n")
	writeFile(t, filepath.Join(root, "keep.go"), "package k\n")
	fs, err := scan.WalkIgnoring(root, []string{"**/*.gen.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got, wantP := paths(fs), []string{"keep.go"}; !reflect.DeepEqual(got, wantP) {
		t.Fatalf("paths = %v, want %v", got, wantP)
	}
	want := []scan.Ignored{
		{Path: "a/util.gen.go", Dir: true, Glob: "**/*.gen.go"},
		{Path: "b/y.gen.go", Dir: false, Glob: "**/*.gen.go"},
	}
	if !reflect.DeepEqual(fs.Ignored, want) {
		t.Fatalf("Ignored = %#v, want %#v", fs.Ignored, want)
	}
}
