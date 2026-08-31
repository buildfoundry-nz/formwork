package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLiveIncludeInventoryIsPacked pins #12398: the live-include registry is
// one packed file, not a per-rule directory. The directory form exploded the
// tracked tree past 2,000 inventories; a count budget on that layout was a
// stall, not a pack. This test fails if the retired directory returns or if
// the pack is missing.
//
// Leftover first-batch / remainder worktrees (#14413) used to carry a
// live-include-globs/ copy. Do not copy those TSV files back onto a branch
// — rows live in live-include-globs.tsv. The leftover worktrees themselves
// are removed; this tree must still have no live-include-globs directory.
func TestLiveIncludeInventoryIsPacked(t *testing.T) {
	root := repoRoot(t)
	// Read from the same vars the census itself uses. Spelling the paths out
	// again pinned one repository's tools/ layout into the test, so the test
	// passed only in the tree it was written in (buildfoundry-nz/formwork#5).
	retiredDir := liveIncludeInventoryDirRel
	packRel := liveIncludeInventoryRel
	dir := filepath.Join(root, filepath.FromSlash(retiredDir))
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		t.Fatalf("%s still exists — the per-rule inventory directory is retired (#12398); rows live in %s",
			retiredDir, packRel)
	}
	pack := filepath.Join(root, filepath.FromSlash(packRel))
	if _, err := os.Stat(pack); err != nil {
		t.Fatalf("packed inventory missing at %s: %v", packRel, err)
	}
}
