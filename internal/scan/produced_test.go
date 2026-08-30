package scan_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// Produced is the seam #98's fix stopped short of (#308). Restrict folds the
// two spellings of one filename onto one file; the CALLER that has to account
// for every requested path — internal/cli's requestedButAbsent — then rebuilt
// its own answer by exact string equality, so on macOS a file the fold DID
// produce was reported as one the scan "never produced", at exit 2, with no
// cure line and a real `git commit` refused.
//
// The fixtures below are the fold's own controls asked as a question, because
// an accounting seam that is wrong in EITHER direction is worse than the bug it
// closes: answering false where the file was produced is #308, and answering
// true where it was not would turn #158's loud refusal into the false pass
// #158 exists to have closed.

// TestProducedAnswersForAFileTheWalkCarriesExactly is the ordinary case, and
// the one that keeps the fold from being the only path through the function: a
// path spelled exactly as the walk carries it is produced, whatever alphabet it
// is in and whatever else is on disk.
func TestProducedAnswersForAFileTheWalkCarriesExactly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, asciiN), "const y = 2\n")
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if !fset.Produced(asciiN) {
		t.Fatalf("Produced(%q) = false, want true: the walk carries that exact path", asciiN)
	}
}

// TestProducedAnswersForTheNormalizationDivergentSpelling is #308 itself. git
// on macOS reports the NFC spelling (core.precomposeunicode, default true) and
// the walk carries the NFD bytes readdir gave it, so the accounting's exact
// match misses a file the scan really did read.
//
// The tree holds a second non-ASCII file that nobody named, mirroring the fold's
// own fixture — but be clear about what THIS test can see: the answer is a
// bool, so an implementation that matched the decoy instead would still say
// true here and still pass. The test that catches a guess is
// TestProducedLeavesAPathTheWorktreeNoLongerHasAbsent below, where the right
// candidate does not exist and only a wrong one is available.
func TestProducedAnswersForTheNormalizationDivergentSpelling(t *testing.T) {
	root := t.TempDir()
	if !normalizationInsensitiveFS(t, root) {
		t.Skip("filesystem is normalization-sensitive: git and the walk report the same bytes, so #98's divergence cannot arise here")
	}
	writeFile(t, filepath.Join(root, nfdName), "const x = 1;\n")
	writeFile(t, filepath.Join(root, decoyN), "const z = 3;\n") // non-ASCII, named by nobody
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if !fset.Produced(nfcName) {
		t.Fatalf("Produced(%+q) = false, want true: the walk read that file under %+q, so a caller "+
			"told otherwise refuses a file the scan did check", nfcName, nfdName)
	}
}

// TestProducedLeavesAPathTheWorktreeNoLongerHasAbsent is the #158 fail-closed
// control. A path git named and the worktree does not have was not scanned by
// anything, and the caller's refusal is the only thing standing between that
// and a silent exit 0 — so the fold must not answer for it. Non-ASCII, so the
// ASCII gate is not what is doing the work, and with a non-ASCII walked file
// present for a guess to land on.
func TestProducedLeavesAPathTheWorktreeNoLongerHasAbsent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, nfdName), "const x = 1;\n") // present, NOT the path asked about
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if fset.Produced(absentN) {
		t.Fatalf("Produced(%+q) = true, want false: that path is on disk under no spelling, and a "+
			"scan cannot speak for a file it never read", absentN)
	}
}

// TestProducedDoesNotAnswerForAnASCIIHardLinkAlias is #333's property on the
// REQUESTED side. os.SameFile is device+inode and a hard link is two names for
// one inode with no normalization anywhere in it, so without the non-ASCII gate
// an all-ASCII path the walk had pruned would be answered for by a walked file
// the caller never named — and here it would be answered for as SCANNED, which
// is the direction that loses coverage.
func TestProducedDoesNotAnswerForAnASCIIHardLinkAlias(t *testing.T) {
	root := t.TempDir()
	walked := filepath.Join(root, nfcName)
	writeFile(t, walked, "const x = 1;\n")
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
	if fset.Produced("vendor/alias.ts") {
		t.Fatalf("Produced(\"vendor/alias.ts\") = true, want false: an all-ASCII path has no "+
			"divergent spelling, and sharing an inode with %+q is not being scanned", nfcName)
	}
}

// TestProducedDoesNotAnswerForANonASCIIHardLinkAlias is the same property on
// the WALKED side: an ASCII walked file has no divergent spelling either, so no
// requested spelling may be answered for by it.
func TestProducedDoesNotAnswerForANonASCIIHardLinkAlias(t *testing.T) {
	root := t.TempDir()
	walked := filepath.Join(root, asciiN)
	writeFile(t, walked, "const y = 2\n")
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
	if fset.Produced("vendor/" + nfcName) {
		t.Fatalf("Produced(%+q) = true, want false: %q is all-ASCII, so it is not a candidate "+
			"spelling for anything", "vendor/"+nfcName, asciiN)
	}
}

// TestProducedDoesNotAnswerForASymlinkPointingAtAWalkedFile pins Lstat over
// Stat on the requested spelling. A symlink is judged by the link and never by
// its target — internal/cli's requestedButAbsent Lstats for exactly that reason
// — so a symlink that points AT a walked file is not that file and was not
// scanned. Under os.Stat this fixture resolves to the target's inode and the
// answer flips to true, which would excuse an unscanned pointer as checked.
//
// The link sits inside an ignored tree because #54 refuses a committed source
// symlink outright everywhere else; inside one the walk consults the glob
// first, which is how such a path reaches an accounting seam at all.
func TestProducedDoesNotAnswerForASymlinkPointingAtAWalkedFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, nfcName), "const x = 1;\n")
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
	if fset.Produced("vendor/" + decoyN) {
		t.Fatalf("Produced(%+q) = true, want false: a symlink is not the file it points at, and "+
			"the walk read %+q rather than it", "vendor/"+decoyN, nfcName)
	}
}
