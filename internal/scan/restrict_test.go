package scan_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// The two byte sequences for one visible filename. Written as \u escapes, not
// as literal characters: a Go source file is saved NFC by every editor
// involved, which is precisely why #97's tests could not see this bug — they
// created their files from NFC literals, so both sides agreed by construction.
const (
	nfcName = "na\u00efve.ts"  // precomposed U+00EF: what git reports on macOS
	nfdName = "nai\u0308ve.ts" // 'i' + combining U+0308: what readdir returns
	asciiN  = "plain.ts"       // no non-ASCII byte, so no normalization to diverge
	absentN = "na\u00efve2.ts" // non-ASCII, and on disk under no spelling at all
	decoyN  = "aper\u00e7u.ts" // non-ASCII, on disk, and named by nobody
)

// normalizationInsensitiveFS reports whether this filesystem resolves the NFC
// spelling of a file created with NFD bytes — i.e. whether #98's divergence can
// arise here at all, and whether the filesystem can therefore serve as the
// oracle for it. macOS/APFS: yes. Linux/ext4: no, and there the divergence does
// not arise either, because git reports the bytes readdir gave it.
//
// Probed rather than keyed on GOOS, mirroring the core.ignorecase skip in
// internal/meta/scanignore_tracked_test.go: the property belongs to the
// filesystem, not to the operating system.
func normalizationInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "probe-"+nfdName)
	writeFile(t, probe, "probe\n")
	_, err := os.Stat(filepath.Join(dir, "probe-"+nfcName))
	if rmErr := os.Remove(probe); rmErr != nil {
		t.Fatal(rmErr)
	}
	return err == nil
}

// TestRestrictMatchesNormalizationDivergentSpelling is #98. git on macOS
// reports the NFC spelling (core.precomposeunicode, default true); the walk
// returns whatever bytes readdir gave it — NFD for a file created decomposed;
// Restrict intersected the two by exact string equality, so the file dropped
// out of both the changed set and the tracked set.
//
// Measured on this branch before the fix, with the real binary: a repo whose
// only violation lives in an NFD-named file exits 1 on a whole-tree `check` and
// 0 on `check --staged` of an unrelated file, because the whole-tree invariant
// rule is evaluated over trackedFileSet's Restrict — which had silently dropped
// it. That arm carries no requestedButAbsent accounting, so nothing on the path
// was loud.
func TestRestrictMatchesNormalizationDivergentSpelling(t *testing.T) {
	root := t.TempDir()
	if !normalizationInsensitiveFS(t, root) {
		t.Skip("filesystem is normalization-sensitive: git and the walk report the same bytes, so #98's divergence cannot arise here")
	}
	writeFile(t, filepath.Join(root, nfdName), "const x = 1;\n")
	writeFile(t, filepath.Join(root, asciiN), "const y = 2;\n")
	// A second non-ASCII file, NOT in the allow set, sorted ahead of the NFD one
	// so that a fold which took the first candidate it had rather than the one
	// the filesystem identifies would take this instead.
	writeFile(t, filepath.Join(root, decoyN), "const z = 3;\n")
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.Files) != 3 {
		t.Fatalf("walk produced %q, want three files", paths(fs))
	}

	// What git names: the precomposed spelling, for the same file on disk.
	got := paths(fs.Restrict(map[string]bool{nfcName: true, asciiN: true}))
	want := []string{nfdName, asciiN} // fs.Files order: "nai\u0308..." < "plain..."
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Restrict = %q, want %q (the file git named is on disk under different bytes; "+
			"dropping it exempts it from every rule, and picking a different file scans one git never named)", got, want)
	}
}

// TestRestrictDoesNotFoldAnExactlyMatchedFileOntoASecondSpelling is the control
// that stops the fold widening what git asked for. Both spellings of one name
// are in the allow set — the shape a repository legitimately carrying BOTH
// would produce — and the single file on disk must come back once, under its
// own spelling, never twice.
func TestRestrictDoesNotFoldAnExactlyMatchedFileOntoASecondSpelling(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, nfdName), "const x = 1;\n")
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.Files) != 1 {
		t.Fatalf("walk produced %q, want one file", paths(fs))
	}
	onDisk := fs.Files[0].Path()
	got := paths(fs.Restrict(map[string]bool{nfcName: true, nfdName: true}))
	if !reflect.DeepEqual(got, []string{onDisk}) {
		t.Fatalf("Restrict = %q, want exactly one entry (%q)", got, onDisk)
	}
}

// TestRestrictLeavesTrulyAbsentPathsAbsent is the fold's fail-open control. A
// requested path that is on disk under NO spelling must stay absent — that
// silence is what requestedButAbsent (#158) reads to refuse at exit 2, so a
// fold that guessed here would turn a loud refusal into a false pass. The tree
// deliberately holds an unrequested non-ASCII file, so a fold that took the
// first candidate it had rather than the one the filesystem identifies would
// have something to take.
func TestRestrictLeavesTrulyAbsentPathsAbsent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, asciiN), "const y = 2;\n")
	writeFile(t, filepath.Join(root, nfdName), "const x = 1;\n") // present, NOT requested
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	got := paths(fs.Restrict(map[string]bool{
		asciiN:    true,
		"gone.ts": true, // ASCII, never existed
		absentN:   true, // non-ASCII, on disk under no spelling
	}))
	if !reflect.DeepEqual(got, []string{asciiN}) {
		t.Fatalf("Restrict = %q, want only %q (a requested path that is on disk under no spelling must stay absent)", got, asciiN)
	}
}

// TestRestrictDoesNotFoldAnASCIIPathOntoAHardLink tests the hasNonASCII gate,
// which was carrying the load-bearing half of the fold's argument — "an
// all-ASCII repository never reaches the fold at all", i.e. no Linux verdict
// can move — with no test that could disagree.
//
// The gate is what keeps identity from widening past normalization. os.SameFile
// is device+inode, and a HARD LINK is two names for one inode with no
// normalization anywhere in sight: an allow path that is on disk but pruned out
// of the walk would otherwise claim a walked file the caller never named, on
// nothing but a shared inode. Every byte here is ASCII, so this runs on every
// filesystem — unlike the NFC/NFD tests above, which need a
// normalization-insensitive one.
//
// WHAT THIS ONE KILLS, EXACTLY: the fixture is all-ASCII on both sides, so the
// walked-side gate empties the candidate set and the requested-side gate is
// never reached. It therefore kills the SIMULTANEOUS removal of the two, and
// neither half alone (#333 measured that: each single-site removal left all 35
// packages green). The two tests below carry a half each.
func TestRestrictDoesNotFoldAnASCIIPathOntoAHardLink(t *testing.T) {
	root := t.TempDir()
	walked := filepath.Join(root, "keep.ts")
	writeFile(t, walked, "export const x = 1\n")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(walked, filepath.Join(root, "vendor", "alias.ts")); err != nil {
		t.Skipf("this filesystem does not support hard links: %v", err)
	}
	fset, err := scan.WalkIgnoring(root, []string{"vendor/**"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(fset); !reflect.DeepEqual(got, []string{"keep.ts"}) {
		t.Fatalf("setup: the walk should carry only the unpruned file, got %v", got)
	}
	got := paths(fset.Restrict(map[string]bool{"vendor/alias.ts": true}))
	if len(got) != 0 {
		t.Fatalf("no walked file was named, and sharing an inode is not being named: got %v", got)
	}
}

// TestRestrictDoesNotFoldAnASCIIHardLinkAliasOntoANonASCIIWalkedFile pins the
// REQUESTED half of the gate on its own — `hasNonASCII(p)` in foldSpellings,
// which decides which allow paths go looking for a divergent spelling.
//
// The test above cannot see this half. Its fixture is all-ASCII on BOTH sides,
// so the walked-side gate empties `unmatched` and foldSpellings returns before
// the requested-side gate is ever evaluated; deleting that gate alone left the
// whole module green (#333). Here the walked file is non-ASCII and unrequested,
// so it reaches `unmatched` legitimately, and the ONLY thing standing between
// the requested ASCII alias and a fold is the gate this test is about.
//
// What the widening would be: `vendor/alias.ts` is a hard link, so it is one
// inode under two names with no normalization anywhere in it. Serving the
// walked file for it would hand the caller a file it never named — and the
// caller is internal/cli's trackedFileSet, which passes the ENTIRE tracked list
// as allow, so every tracked-but-pruned path would be a claimant.
func TestRestrictDoesNotFoldAnASCIIHardLinkAliasOntoANonASCIIWalkedFile(t *testing.T) {
	root := t.TempDir()
	walked := filepath.Join(root, nfcName) // non-ASCII, and named by nobody
	writeFile(t, walked, "export const x = 1\n")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(walked, filepath.Join(root, "vendor", "alias.ts")); err != nil {
		t.Skipf("this filesystem does not support hard links: %v", err)
	}
	fset, err := scan.WalkIgnoring(root, []string{"vendor/**"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(fset); !reflect.DeepEqual(got, []string{nfcName}) {
		t.Fatalf("setup: the walk should carry only the unpruned file, got %v", got)
	}
	got := paths(fset.Restrict(map[string]bool{"vendor/alias.ts": true}))
	if len(got) != 0 {
		t.Fatalf("an all-ASCII requested path has no divergent spelling to look for, "+
			"and sharing an inode is not being named: got %v", got)
	}
}

// TestRestrictDoesNotFoldANonASCIIHardLinkAliasOntoAnASCIIWalkedFile pins the
// WALKED half of the gate on its own — `hasNonASCII(f.Path())` in Restrict,
// which decides which walked files become fold candidates.
//
// It is the mirror of the test above: the requested alias is non-ASCII, so it
// passes the requested-side gate and `wanted` is non-empty, and the walked file
// it shares an inode with is ASCII. An ASCII walked path has no divergent
// spelling either, so it must never be a candidate — otherwise a non-ASCII
// pruned alias claims it, and again on nothing but a shared inode.
//
// Together with the two above, each call site now has an independent mutation
// proof; before #333 only the simultaneous removal was killed by anything.
func TestRestrictDoesNotFoldANonASCIIHardLinkAliasOntoAnASCIIWalkedFile(t *testing.T) {
	root := t.TempDir()
	walked := filepath.Join(root, asciiN) // ASCII, and named by nobody
	writeFile(t, walked, "export const y = 2\n")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(walked, filepath.Join(root, "vendor", nfcName)); err != nil {
		t.Skipf("this filesystem does not support hard links: %v", err)
	}
	fset, err := scan.WalkIgnoring(root, []string{"vendor/**"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(fset); !reflect.DeepEqual(got, []string{asciiN}) {
		t.Fatalf("setup: the walk should carry only the unpruned file, got %v", got)
	}
	got := paths(fset.Restrict(map[string]bool{"vendor/" + nfcName: true}))
	if len(got) != 0 {
		t.Fatalf("an all-ASCII walked path has no divergent spelling, so it is not a fold "+
			"candidate for any spelling: got %v", got)
	}
}

// TestRestrictDoesNotFoldASymlinkOntoAWalkedFile is the #333 widening reached
// through a mechanism git DOES create.
//
// The two tests above pin the hasNonASCII gate, and #333's own record scoped
// its exposure to "an out-of-band hard link git itself never creates". A
// SYMLINK is not out of band: git tracks one as mode 120000, so a committed
// non-ASCII symlink arrives in `allow` on every ordinary run of --staged,
// --range and trackedFileSet. The gate does not stop it — the link's own name
// is non-ASCII, so it passes — and os.Stat FOLLOWS, so the requested spelling
// resolved to the TARGET's inode and os.SameFile then agreed with a walked
// file the caller never named. Measured on this tree before the fix:
// Restrict({vendor/aperçu.ts}) returned [naïve.ts].
//
// WHAT THE WIDENING COSTS, and it is not symmetrical with the hard-link case.
// The caller is internal/cli, and trackedFileSet passes the ENTIRE tracked list
// as allow — so a tracked symlink pointing at an UNTRACKED walked file folds
// that file into the tracked set, and #23's asymmetry ("an untracked file must
// not SATISFY an armed scope floor") is back with a pointer as the mechanism.
// On the --staged arm it is the other error: a file nobody staged is scanned
// and its findings refuse the commit.
//
// A SYMLINK IS JUDGED BY THE LINK AND NEVER BY ITS TARGET is the rule the rest
// of this engine already keeps — (*FileSet).Produced Lstats for exactly this
// reason and says so, internal/cli's requestedButAbsent Lstats and says so, and
// the walk classifies from the directory entry. foldSpellings was the one seam
// that followed, which also made Restrict and Produced — the two halves of one
// accounting — disagree about the same path.
//
// The link sits inside an ignored tree because #54 refuses a committed source
// symlink outright everywhere else; inside one the walk consults the glob
// first, which is how such a path reaches the fold at all — the same fixture
// shape TestProducedDoesNotAnswerForASymlinkPointingAtAWalkedFile uses, asked
// of the other half of the seam.
func TestRestrictDoesNotFoldASymlinkOntoAWalkedFile(t *testing.T) {
	root := t.TempDir()
	walked := filepath.Join(root, nfcName) // non-ASCII, and named by nobody
	writeFile(t, walked, "export const x = 1\n")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", nfcName), filepath.Join(root, "vendor", decoyN)); err != nil {
		t.Skipf("this filesystem does not support symlinks: %v", err)
	}
	fset, err := scan.WalkIgnoring(root, []string{"vendor/**"})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(fset); !reflect.DeepEqual(got, []string{nfcName}) {
		t.Fatalf("setup: the walk should carry only the unpruned regular file, got %v", got)
	}
	got := paths(fset.Restrict(map[string]bool{"vendor/" + decoyN: true}))
	if len(got) != 0 {
		t.Fatalf("a symlink is not the file it points at, so no walked file was named: got %v", got)
	}
}
