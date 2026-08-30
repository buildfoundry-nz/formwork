package scan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// gitIgnored builds a prune set the way a caller's git seam would. Each entry
// is "path" for a file or "path/" for a directory; the rule string stands in
// for the .gitignore line that decided it.
func gitIgnored(entries ...string) *scan.GitIgnored {
	g := scan.NewGitIgnored()
	for _, e := range entries {
		dir := strings.HasSuffix(e, "/")
		g.Add(strings.TrimSuffix(e, "/"), dir, ".gitignore:1:"+strings.TrimSuffix(e, "/"))
	}
	return g
}

func TestWalkWithPrunesGitignoredFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.go"), "package a\n")
	writeFile(t, filepath.Join(root, ".turn-state.txt"), "package a\n")

	fs, err := scan.WalkWith(root, scan.Opts{GitIgnored: gitIgnored(".turn-state.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"keep.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	want := []scan.Ignored{{
		Path: ".turn-state.txt", Dir: false,
		By: scan.SourceGitignore, Rule: ".gitignore:1:.turn-state.txt",
	}}
	if !reflect.DeepEqual(fs.Ignored, want) {
		t.Fatalf("Ignored = %#v, want %#v", fs.Ignored, want)
	}
}

func TestWalkWithPrunesGitignoredDirWithoutDescending(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "x", "build", "a.js"), "x\n")
	writeFile(t, filepath.Join(root, "packages", "x", "lib.dart"), "// a\n")

	fs, err := scan.WalkWith(root, scan.Opts{GitIgnored: gitIgnored("packages/x/build/")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"packages/x/lib.dart"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	// One Dir record for the pruned root; nothing below it is enumerated, so
	// the prune costs nothing on a tree carrying many ignored checkouts.
	if len(fs.Ignored) != 1 || !fs.Ignored[0].Dir || fs.Ignored[0].Path != "packages/x/build" {
		t.Fatalf("Ignored = %#v, want one Dir record for packages/x/build", fs.Ignored)
	}
}

// The prune set is exact: a path git did not confirm ignored is scanned, even
// when it sits beside one that was. This is what keeps the mechanism narrower
// than "skip what is untracked" (#80).
func TestWalkWithScansPathsNotInThePruneSet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "build", "out.js"), "x\n")
	writeFile(t, filepath.Join(root, "new-but-unstaged.go"), "package a\n")

	fs, err := scan.WalkWith(root, scan.Opts{GitIgnored: gitIgnored("build/")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"new-but-unstaged.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

// With no prune set declared the walk is byte-identical to today's. This is the
// contract that lets the feature ship opt-in: a repo that does not declare
// scan.gitignore cannot be affected by any of it.
func TestWalkWithNilGitIgnoredEqualsWalkIgnoring(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, "gen", "b.gen.go"), "package b\n")
	writeFile(t, filepath.Join(root, ".git", "config"), "x\n")

	a, err := scan.WalkIgnoring(root, []string{"**/*.gen.go"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := scan.WalkWith(root, scan.Opts{Ignore: []string{"**/*.gen.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths(a), paths(b)) {
		t.Fatalf("paths differ: %v vs %v", paths(a), paths(b))
	}
	if !reflect.DeepEqual(a.Ignored, b.Ignored) {
		t.Fatalf("Ignored differs: %#v vs %#v", a.Ignored, b.Ignored)
	}
}

// Both channels can prune in one walk, and each record names the channel that
// made the decision — the census cannot report a gitignore prune as an
// operator-declared one, or the reverse.
func TestWalkWithRecordsEachChannelSeparately(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.go"), "package a\n")
	writeFile(t, filepath.Join(root, "gen", "x.gen.go"), "package b\n")
	writeFile(t, filepath.Join(root, "scratch.txt"), "x\n")

	fs, err := scan.WalkWith(root, scan.Opts{
		Ignore:     []string{"**/*.gen.go"},
		GitIgnored: gitIgnored("scratch.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(fs), []string{"keep.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	bySource := map[scan.Source]string{}
	for _, ig := range fs.Ignored {
		bySource[ig.By] = ig.Path
	}
	if bySource[scan.SourceIgnore] != "gen/x.gen.go" || bySource[scan.SourceGitignore] != "scratch.txt" {
		t.Fatalf("Ignored = %#v, want one record per channel", fs.Ignored)
	}
}

// Same trade as #54 inside a scan.ignore tree: git has refused the path, so
// the walk must not die on a source symlink underneath it.
func TestWalkWithSourceSymlinkInsideGitignoredTreeDoesNotError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "build", "real.go"), "package v\n")
	if err := os.Symlink("real.go", filepath.Join(root, "build", "alias.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeFile(t, filepath.Join(root, "main.go"), "package a\n")

	fs, err := scan.WalkWith(root, scan.Opts{GitIgnored: gitIgnored("build/")})
	if err != nil {
		t.Fatalf("walk errored on symlink inside gitignored tree: %v", err)
	}
	if got, want := paths(fs), []string{"main.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

// Regression guard: pruning must not weaken #54 anywhere it still applies.
func TestWalkWithSourceSymlinkOutsideGitignoreStillRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.go"), "package a\n")
	if err := os.Symlink("real.go", filepath.Join(root, "alias.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := scan.WalkWith(root, scan.Opts{GitIgnored: gitIgnored("build/")})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v, want symlink refusal", err)
	}
}
