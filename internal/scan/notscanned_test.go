package scan_test

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// TestNotScannedByAttributesAtWalkOrder pins the shallowest-trigger,
// builtin-before-glob-per-level attribution NotScannedBy shares with the
// walk: a .git nested under an ignored tree is the OPERATOR's hiding (the
// walk pruned above it), a root-level .git is the built-in's, and an
// unhidden path is neither.
func TestNotScannedByAttributesAtWalkOrder(t *testing.T) {
	opts := scan.Opts{Ignore: []string{"third_party/**"}}

	v := scan.NotScannedBy("third_party/dep/.git/config", opts)
	if !v.Hidden() || v.Builtin || v.Glob != "third_party/**" {
		t.Fatalf("nested .git under ignored tree: %+v, want operator glob", v)
	}

	v = scan.NotScannedBy(".git/config", opts)
	if !v.Hidden() || !v.Builtin || v.Glob != "" {
		t.Fatalf("root .git: %+v, want builtin", v)
	}

	// Built-in shallower than a deeper glob match: builtin wins.
	v = scan.NotScannedBy(".formwork/rules/gen/x.yaml", scan.Opts{Ignore: []string{"**/gen/**"}})
	if !v.Hidden() || !v.Builtin {
		t.Fatalf("builtin above glob: %+v, want builtin", v)
	}

	if v := scan.NotScannedBy("src/x.go", opts); v.Hidden() {
		t.Fatal("unhidden path reported hidden")
	}
}

// TestNotScannedByLeafIsAFile pins the dir-only semantics of the builtin
// skip: WalkIgnoring consults skipDirs in its directory branch and in its
// symlink refusal, never for a regular file — so a regular FILE named .git (a
// linked-worktree gitdir pointer) is scanned and
// must not be reported hidden — while the same name as an ANCESTOR is the
// builtin skip.
func TestNotScannedByLeafIsAFile(t *testing.T) {
	if v := scan.NotScannedBy("sub/.git", scan.Opts{}); v.Hidden() {
		t.Fatal("leaf segment named .git is a file to the walk — not hidden")
	}
	if v := scan.NotScannedBy("sub/.git/config", scan.Opts{}); !v.Hidden() || !v.Builtin {
		t.Fatal("ancestor .git must stay builtin-hidden")
	}
	// Leaf still matches ignore GLOBS — the walk's file branch checks them.
	if v := scan.NotScannedBy("gen/out.txt", scan.Opts{Ignore: []string{"gen/out.txt"}}); !v.Hidden() || v.Glob != "gen/out.txt" {
		t.Fatal("leaf-level glob match must still hide")
	}
}

// TestNotScannedByGitignoreChannel pins the third hiding channel (#122) at
// the walk's own order: within a level skipDirs, then scan.ignore globs, then
// the gitignore prune (WalkWith's pinned ordering); across levels the
// shallowest trigger wins whatever its channel. Attribution carries the
// deciding ignore rule in the census's own "<file>:<line>:<pattern>" shape.
func TestNotScannedByGitignoreChannel(t *testing.T) {
	gi := scan.NewGitIgnored()
	gi.Add("vendor", true, ".gitignore:1:vendor/")
	gi.Add("gen/out.bin", false, ".gitignore:2:*.bin")

	// An ignored ANCESTOR directory hides everything beneath it.
	v := scan.NotScannedBy("vendor/sub/x.go", scan.Opts{GitIgnored: gi})
	if !v.Hidden() || v.Builtin || v.Glob != "" || v.GitRule != ".gitignore:1:vendor/" {
		t.Fatalf("under ignored dir: %+v, want gitignore rule", v)
	}

	// An ignored LEAF file hides just itself — via the files set, mirroring
	// the walk's file branch.
	v = scan.NotScannedBy("gen/out.bin", scan.Opts{GitIgnored: gi})
	if !v.Hidden() || v.GitRule != ".gitignore:2:*.bin" {
		t.Fatalf("ignored leaf file: %+v, want gitignore rule", v)
	}

	// A dirs entry must NOT hide a leaf: the walk consults the files set in
	// its file branch, and a queried path is a file path.
	giDirOnly := scan.NewGitIgnored()
	giDirOnly.Add("gen/out.bin", true, ".gitignore:9:bogus")
	if v := scan.NotScannedBy("gen/out.bin", scan.Opts{GitIgnored: giDirOnly}); v.Hidden() {
		t.Fatalf("dirs entry hid a leaf file: %+v", v)
	}

	// Same level, both channels: scan.ignore is checked before gitignore,
	// exactly as WalkWith orders them.
	v = scan.NotScannedBy("vendor/sub/x.go", scan.Opts{Ignore: []string{"vendor/**"}, GitIgnored: gi})
	if !v.Hidden() || v.Glob != "vendor/**" || v.GitRule != "" {
		t.Fatalf("glob-before-gitignore per level: %+v, want glob", v)
	}

	// Shallower gitignore dir beats a deeper glob: the walk prunes at the
	// shallow level before the deeper level's glob is ever consulted.
	giShallow := scan.NewGitIgnored()
	giShallow.Add("a", true, ".gitignore:1:a/")
	v = scan.NotScannedBy("a/b/x.go", scan.Opts{Ignore: []string{"a/b/**"}, GitIgnored: giShallow})
	if !v.Hidden() || v.Glob != "" || v.GitRule != ".gitignore:1:a/" {
		t.Fatalf("shallow gitignore above deeper glob: %+v, want gitignore rule", v)
	}

	// A nil set covers nothing — the undeclared repo takes the same path as
	// before the key existed.
	if v := scan.NotScannedBy("vendor/sub/x.go", scan.Opts{}); v.Hidden() {
		t.Fatalf("nil GitIgnored hid a path: %+v", v)
	}
}

// TestNotScannedByGitignoredSymlinkAncestor pins channel parity for the one
// shape where a gitignore ANCESTOR lives in the files set: git lists a
// symlink (even one pointing at a directory) as a non-dir entry, and the
// walk meets it in its FILE branch — pruning it at the gitignore check
// before the non-regular skip, so the census attributes SourceGitignore.
// Attribution here must name the same channel, not fall through to a
// non-regular diagnosis two trusted surfaces would then disagree about
// (#125 round-2 finding 1).
func TestNotScannedByGitignoredSymlinkAncestor(t *testing.T) {
	gi := scan.NewGitIgnored()
	gi.Add("symdir", false, ".gitignore:1:symdir")

	v := scan.NotScannedBy("symdir/x.go", scan.Opts{GitIgnored: gi})
	if !v.Hidden() || v.GitRule != ".gitignore:1:symdir" {
		t.Fatalf("under gitignored file-entry ancestor: %+v, want gitignore rule", v)
	}
	// Per-level order holds: a glob at the same level still wins, as the
	// walk's file branch checks globs first.
	v = scan.NotScannedBy("symdir/x.go", scan.Opts{Ignore: []string{"symdir"}, GitIgnored: gi})
	if !v.Hidden() || v.Glob != "symdir" || v.GitRule != "" {
		t.Fatalf("glob-before-gitignore at a file-entry ancestor: %+v, want glob", v)
	}
}

// TestGitIgnoredLookup pins the exported read the guidance layer's ghost
// overlay needs: exact-frame lookups against the snapshot, nil-safe.
func TestGitIgnoredLookup(t *testing.T) {
	gi := scan.NewGitIgnored()
	gi.Add("vendor", true, ".gitignore:1:vendor/")
	if rule, ok := gi.Lookup("vendor", true); !ok || rule != ".gitignore:1:vendor/" {
		t.Fatalf("dir lookup = %q,%v", rule, ok)
	}
	if _, ok := gi.Lookup("vendor", false); ok {
		t.Fatal("frame must not cross: dir entry answered a file lookup")
	}
	var nilSet *scan.GitIgnored
	if _, ok := nilSet.Lookup("vendor", true); ok {
		t.Fatal("nil set covers nothing")
	}
}
