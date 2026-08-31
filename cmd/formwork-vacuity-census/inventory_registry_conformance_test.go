package main

// inventory_registry_conformance_test.go — the committed packed registry is
// RENDER-NORMALISED: stable sort, no duplicate rows, canonical header and
// trailing newline (#13521, #12398, #15918).
//
// WHAT THIS FILE DOES NOT PROVE, and used to claim it did. Both tests here
// parse the committed file and re-render WHAT THEY JUST READ, so the expected
// side derives from the artifact under test: they compare the file to itself.
// That can only ever falsify normalisation — ordering, dedup, formatting. It
// cannot detect a registry that has drifted from the LIVE RULE SCOPES, because
// the live scopes are never consulted here.
//
// Agreement with the live scopes is a real invariant and it IS enforced —
// elsewhere, and properly, by inventoryVerdicts (inventory.go): GLOB-UNTRACKED
// for a live glob missing from the registry, GLOB-REMOVED for a row whose rule
// no longer declares the glob. Both are gating, so a stale registry fails the
// census; it does not slip through. Re-asserting that here would mean building
// a whole-tree globMeasure inside a unit test to duplicate a gate that already
// runs.
//
// So normalisation is the honest scope of this file, and it is worth pinning
// on its own terms: live-include-globs.tsv is ~3,800 rows, and a regeneration
// that emitted them in a different order would produce an unreviewable diff.
// Nothing else asserts that.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// driftReportCap bounds how many offending registries are named individually.
const driftReportCap = 5

// repoRoot walks up from the test's working directory to the checkout root,
// identified by .formwork/rules.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, ".formwork", "rules")); err == nil && st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not inside a checkout — no .formwork/rules above the test's working directory")
		}
		dir = parent
	}
}

// TestIncludeRegistryCorpusIsRenderNormalised pins the packed live-include
// registry to the renderer's output for the rows it already contains — sort
// order, dedup, header, trailing newline. It says nothing about whether those
// rows are the right rows; see the file header.
func TestIncludeRegistryCorpusIsRenderNormalised(t *testing.T) {
	root := repoRoot(t)
	abs := filepath.Join(root, filepath.FromSlash(liveIncludeInventoryRel))
	inv, present, err := loadLiveIncludeInventory(root)
	if err != nil {
		t.Fatalf("load %s: %v", liveIncludeInventoryRel, err)
	}
	if !present {
		t.Fatalf("%s is missing — the walk proved nothing", liveIncludeInventoryRel)
	}
	var pairs []inventoryPair
	for p := range inv {
		pairs = append(pairs, p)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", liveIncludeInventoryRel, err)
	}
	if !bytes.Equal(got, renderIncludeRegistry(pairs)) {
		t.Errorf("%s is not RENDER-NORMALISED — its rows are mis-sorted, duplicated, or its "+
			"header/trailing newline drifted, so any regeneration hands the next author a diff "+
			"full of rows they did not touch (#13521). This is NOT a staleness check: rows "+
			"disagreeing with the live rule scopes are GLOB-UNTRACKED/GLOB-REMOVED at census "+
			"time. Re-render with:\n"+
			"  go run -C tools/formwork-vacuity-census . --write-inventory <repo-root>", liveIncludeInventoryRel)
	}
}

// TestWriteIfChangedSkipsIdenticalContent is the writer-side half.
func TestWriteIfChangedSkipsIdenticalContent(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "some-rule.tsv")
	body := []byte("# header line\nalpha/**\nbeta/**\n")

	wrote, err := writeIfChanged(dst, body)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !wrote {
		t.Fatalf("writeIfChanged reported no write for a file that did not exist — nothing would ever be created")
	}

	wrote, err = writeIfChanged(dst, body)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if wrote {
		t.Errorf("writeIfChanged rewrote a file whose bytes already matched — that is the churn of #13521")
	}

	wrote, err = writeIfChanged(dst, []byte("# header line\nalpha/**\ngamma/**\n"))
	if err != nil {
		t.Fatalf("changed write: %v", err)
	}
	if !wrote {
		t.Errorf("writeIfChanged skipped a file whose content genuinely changed")
	}
}
