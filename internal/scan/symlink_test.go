package scan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestWalkRefusesDirectorySymlink is #143 row 2, the sharpest member of that
// class: `lib -> ../shared/lib` inside a real root. d.IsDir() is false for a
// symlink-to-directory, so the entry fell through to the non-regular branch,
// #54's refusal did not fire (the link's own name carries no source
// extension), and the WHOLE SUBTREE left the walk with no error, no census
// record and no finding — `1/1 rules passed`, exit 0, over a tree holding a
// violation. lint's empty-scope cannot catch it either, because the rule still
// matched other files.
//
// Measured before the fix on this tree via the real binary: `check` exited 0
// with `1 file(s) scanned` while the same violation under a real directory
// exits 1.
func TestWalkRefusesDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "lib", "bad.go"), "package lib\nconst Token = 1\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	if err := os.Symlink(filepath.Join(outside, "lib"), filepath.Join(root, "lib")); err != nil {
		t.Fatal(err)
	}
	_, err := scan.Walk(root)
	if err == nil {
		t.Fatal("expected Walk to refuse a directory symlink; got nil error (the subtree vanishing at exit 0 is the bug)")
	}
	if !strings.Contains(err.Error(), "lib") {
		t.Fatalf("error should name the symlink path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error should identify the problem as a symlink, got: %v", err)
	}
}

// TestWalkRefusesExtensionlessSourceSymlink is #143 row 3: `Makefile ->
// ../shared/Makefile`. isSourceSymlinkName keyed on filepath.Ext alone, so an
// extensionless build file — which make, and any rule scoping `**/Makefile`,
// reads exactly like source — was skipped in silence. The same lever as #54,
// one name-shape wider.
func TestWalkRefusesExtensionlessSourceSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "shared.mk"), "all:\n\techo hi\n")
	if err := os.Symlink("shared.mk", filepath.Join(root, "Makefile")); err != nil {
		t.Fatal(err)
	}
	_, err := scan.Walk(root)
	if err == nil {
		t.Fatal("expected Walk to refuse a Makefile symlink; got nil error (silent skip is the bug)")
	}
	if !strings.Contains(err.Error(), "Makefile") {
		t.Fatalf("error should name the symlink path, got: %v", err)
	}
}

// TestWalkResolvesSymlinkedRoot is the walk's half of #143 row 1. WalkDir
// lstats the root, so a symlinked root arrived at the callback as a non-dir
// entry, was passed over by the `p == root` early return, and the walk ended
// having enumerated nothing — `0 file(s) scanned`, `1/1 rules passed`, exit 0.
//
// The remedy is RESOLVE, not refuse: PR #148 refused and was closed unmerged,
// because one byte later (`-C alias/`) the same tree scans correctly, so the
// walk can serve a symlinked root once its final component is resolved.
// Resolving the root costs no traversal semantics — symlinks INSIDE the tree
// are still never followed, which TestWalkRefusesDirectorySymlink above pins.
//
// This does NOT discharge the issue's criterion 2 (one root contract at the -C
// seam, so `rules-for`/`scope`/`explain` agree). Those subcommands do not call
// the walk; this fixes every consumer that does.
func TestWalkResolvesSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeFile(t, filepath.Join(real, "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(real, "README.md"), "# hi\n")
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	fs, err := scan.Walk(alias)
	if err != nil {
		t.Fatalf("Walk of a symlinked root: %v", err)
	}
	want := []string{"README.md", "src/a.go"}
	if got := paths(fs); !reflect.DeepEqual(got, want) {
		t.Fatalf("symlinked root scanned %v, want %v", got, want)
	}
	// Criterion 3: `-C alias` and `-C alias/` behave identically. The trailing
	// slash made lstat resolve the link, which is why only one of the two
	// spellings was ever broken.
	withSlash, err := scan.Walk(alias + string(os.PathSeparator))
	if err != nil {
		t.Fatalf("Walk of alias/: %v", err)
	}
	if got := paths(withSlash); !reflect.DeepEqual(got, want) {
		t.Fatalf("alias/ scanned %v, want %v", got, want)
	}
	// Content must still be readable through the resolved root.
	if b, err := fs.Files[1].Content(); err != nil || string(b) != "package a\n" {
		t.Fatalf("content through a resolved root: %q, %v", b, err)
	}
	// Root is reported as GIVEN, not as resolved: it is what rule finalizers
	// join their own paths against (rules.FinalizeContext{Root: fset.Root}),
	// and the alias resolves for them exactly as it does here.
	if fs.Root != alias {
		t.Fatalf("FileSet.Root = %q, want the root as given (%q)", fs.Root, alias)
	}
}

// TestWalkRefusesRootThatIsNotADirectory closes the last silent arm of the same
// early return: handed a regular file, or a symlink to one, WalkDir enumerated
// nothing and the walk reported a clean empty tree. There is no directory to
// enumerate in either case, so the only honest answer is an error.
func TestWalkRefusesRootThatIsNotADirectory(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "a.go")
	writeFile(t, file, "package a\n")
	link := filepath.Join(base, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{file, link} {
		if _, err := scan.Walk(root); err == nil {
			t.Fatalf("Walk(%q) returned no error; an empty FileSet reads as a clean tree", root)
		}
	}
}

// TestWalkRefusesRootSymlinkThatDoesNotResolve: a dangling root is "I cannot
// look here", which must not read as "I looked and it was fine" either.
func TestWalkRefusesRootSymlinkThatDoesNotResolve(t *testing.T) {
	base := t.TempDir()
	link := filepath.Join(base, "alias")
	if err := os.Symlink(filepath.Join(base, "nowhere"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := scan.Walk(link); err == nil {
		t.Fatal("Walk of a dangling root symlink returned no error")
	}
}

// TestWalkMissingRootStillReportsWalkDirsError pins that resolving the root did
// not change the error a caller already gets for a root that is simply not
// there — the lstat is a refinement of the walk, not a new gate in front of it.
func TestWalkMissingRootStillReportsWalkDirsError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	_, err := scan.Walk(root)
	if err == nil {
		t.Fatal("Walk of a missing root returned no error")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("Walk of a missing root: err = %v, want a not-exist error", err)
	}
}

// TestWalkIgnoringDeclaredSymlinksAreNotRefused extends the trade
// ignore_test.go pins for #54 to both refusals added here: where a scan.ignore
// glob names the entry, the operator has declared it not theirs to gate, so the
// walk records it and moves on rather than dying on it. Both new refusals sit
// AFTER the prune checks in the file branch for that reason.
//
// The globs deliberately match the ENTRIES, not a containing directory: a
// directory-shaped glob prunes at the d.IsDir() branch and the walk never
// descends, so it would exercise no ordering in the file branch at all.
func TestWalkIgnoringDeclaredSymlinksAreNotRefused(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "lib", "bad.go"), "package lib\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "shared.mk"), "all:\n")
	if err := os.Symlink(filepath.Join(outside, "lib"), filepath.Join(root, "lib")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("shared.mk", filepath.Join(root, "Makefile")); err != nil {
		t.Fatal(err)
	}
	fs, err := scan.WalkIgnoring(root, []string{"lib", "Makefile"})
	if err != nil {
		t.Fatalf("walk errored on symlinks a scan.ignore glob names: %v", err)
	}
	if got, want := paths(fs), []string{"main.go", "shared.mk"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	// Both are censused, so the declaration stays visible rather than silent.
	if len(fs.Ignored) != 2 {
		t.Fatalf("Ignored = %#v, want both symlinks recorded", fs.Ignored)
	}
}

// TestWalkSkipsDirectorySymlinkPointingInsideTheTree is the control that keeps
// the row-2 refusal to the shape that actually costs coverage. `alias -> src`
// hides nothing: the walk already enumerates src by its own name, so every file
// behind the link is scanned and enforced on under a path of its own. Refusing
// there would fail repositories over a link that exempts no content — and
// internal/cli's TestCheckStagedSymlinkIsNotRefusedAsAbsent builds exactly this
// tree, so the narrowing is a contract two packages depend on rather than a
// judgement call made here.
func TestWalkSkipsDirectorySymlinkPointingInsideTheTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "a.go"), "package a\n")
	if err := os.Symlink("src", filepath.Join(root, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatalf("walk refused a directory symlink whose target it already scans: %v", err)
	}
	if got, want := paths(fs), []string{"src/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

// TestWalkSkipsBuiltinSkipDirSymlink pins the FAIL-CLOSED REGRESSION the
// directory-symlink refusal introduced. skipDirs was consulted only in the
// d.IsDir() branch, and a symlink-to-directory reports d.IsDir() false — so
// `.git` or `.formwork` spelled as a symlink fell past the built-in skip into
// the refusal and hard-failed the walk at exit 2, with no override. Both
// spellings are ordinary: a linked worktree or submodule carries `.git` as a
// file or link, and sharing one rule set across a monorepo by symlinking
// `.formwork` is the shape the engine invites.
//
// The refusal's own rationale does not reach here. It exists because the
// subtree behind the link leaves the walk with no error and no census record —
// but these two subtrees are the ones the walk is DEFINED never to enumerate,
// so nothing is lost by not looking, and NotScannedBy already answers
// "hidden, by the built-in skip" for paths beneath them (spec §11 carves the
// same exception).
func TestWalkSkipsBuiltinSkipDirSymlink(t *testing.T) {
	for _, name := range []string{".git", ".formwork"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "shared", "rules.yaml"), "rules: []\n")
			writeFile(t, filepath.Join(root, "main.go"), "package main\n")
			if err := os.Symlink(filepath.Join(outside, "shared"), filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
			fset, err := scan.Walk(root)
			if err != nil {
				t.Fatalf("a %s symlink must not fail the walk; got: %v", name, err)
			}
			if got := paths(fset); !reflect.DeepEqual(got, []string{"main.go"}) {
				t.Fatalf("walk should still enumerate the real tree, got %v", got)
			}
		})
	}
}

// TestWalkRefusesUnstatableDirectorySymlink is the FAIL-OPEN REINTRODUCED
// inside the guard written to close #143: hidesASubtree answered false on ANY
// os.Stat error, and the comment justified it as "a dangling link has no
// content behind it". Stat fails for more reasons than ENOENT. Behind an
// unsearchable parent it fails with EACCES — reachable with no hostile setup on
// macOS, where a Go process is denied ~/Documents until TCC grants it — and
// there IS a subtree behind the link. "I cannot look" was being reported as
// "there is nothing there": #143's exact signature.
//
// The root arm of the same shape (resolveRoot) already refuses a permission
// error; this makes the inner arm agree with it.
func TestWalkRefusesUnstatableDirectorySymlink(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root traverses an unsearchable directory, so the probe cannot arise")
	}
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "closed", "lib", "bad.go"), "package lib\nconst Token = 1\n")
	closed := filepath.Join(outside, "closed")
	if err := os.Chmod(closed, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o700) })
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	if err := os.Symlink(filepath.Join(closed, "lib"), filepath.Join(root, "lib")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib")); err == nil {
		t.Skip("this filesystem lets the process stat through a 0700-cleared directory")
	}
	_, err := scan.Walk(root)
	if err == nil {
		t.Fatal("expected Walk to refuse a symlink it cannot look through; got nil error (an unexamined subtree read as an empty one)")
	}
	if !strings.Contains(err.Error(), "lib") {
		t.Fatalf("error should name the symlink path, got: %v", err)
	}
}

// TestWalkSkipsDanglingSymlink is the other half of that trade, and the one the
// comment's claim is actually true about: a link whose target does not exist
// (ENOENT) has no content behind it to go unscanned, so skipping it is a true
// answer rather than an unexamined one. Without this the refusal above could be
// written as "any Stat error refuses" and no test would disagree.
func TestWalkSkipsDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatalf("a dangling symlink names nothing to scan and must not fail the walk; got: %v", err)
	}
	if got := paths(fset); !reflect.DeepEqual(got, []string{"main.go"}) {
		t.Fatalf("walk should still enumerate the real tree, got %v", got)
	}
}
