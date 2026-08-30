// two_trees_test.go — this package crosses the publication cut, so every
// assertion in it is made about TWO trees: this one, and the tree `make
// publication-cut` materialises from it. The two are not the same tree, and a
// floor written for one is wrong for the other.
//
// That is not a hypothetical. The floors below named
// tools/publication/public-AGENTS.md, its CLAUDE.md twin, and the citations
// this repo's own agent instructions make into internal/cli — every one of
// them a path the manifest drops or a document the cut REPLACES. The published
// tree's `make verify` failed on all three, which is the readiness signal the
// whole publication step turns on, while publication-check, `go build ./...`
// and `go vet ./...` all came back green. `make publication-cut-proof` is what
// asks the question directly now; this file is what makes the answer yes.
//
// They live together because they share the discriminator, and a second copy
// of that predicate is the way the two halves drift apart.
package repoproof_test

import (
	"os"
	"path/filepath"
	"testing"
)

// publishedDocFloor is the non-vacuity floor: the documents that must be in the
// judged set for a green here to mean anything. Two of them are the ones that
// rotted. If a rename drops one out of the derived set, the count check below
// goes red rather than quietly judging a smaller tree.
var publishedDocFloor = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"CONTRIBUTING.md",
	"NOTICE",
	"README.md",
	"SECURITY.md",
	"VERSIONING.md",
	"docs/quickstart.md",
	"docs/reference.md",
	"docs/rule-authoring.md",
}

// publishedDocFloorDev is the rest of the floor in the DEVELOPMENT tree. The
// two overlays are the cut's sources for AGENTS.md and CLAUDE.md above: `make
// publication-cut` materialises them under those names and drops
// tools/publication with the rest of the cut machinery, so the published tree
// has no such path and requiring one there would fail a tree that is correct.
//
// This package CROSSES the cut, so every assertion in it is made about two
// different trees — and the cut tree's `make verify` is the readiness signal
// the whole publication step turns on. Splitting the floor is what lets each
// tree be held to the floor it actually has instead of the union of both.
var publishedDocFloorDev = []string{
	"tools/publication/public-AGENTS.md",
	"tools/publication/public-CLAUDE.md",
}

// cutMachinery is the directory the manifest drops along with the rest of the
// publication tooling. Its presence is what tells the two trees apart.
const cutMachinery = "tools/publication"

// isDevelopmentTree answers which of the two trees this run is judging.
//
// It asks for the cut MACHINERY rather than for either document the floor
// gates, and that is the difference between a discriminator and a tautology:
// keying "must the overlays be here" on the overlays being here would make the
// requirement satisfy itself, and deleting one would go quietly green in both
// trees. With tools/publication as the subject, an overlay deleted from the
// development tree still reddens, because the directory it lived in is still
// standing.
func isDevelopmentTree(t *testing.T) bool {
	t.Helper()
	info, err := os.Stat(filepath.Join(repoRoot(t), filepath.FromSlash(cutMachinery)))
	return err == nil && info.IsDir()
}

// citationFloor is the non-vacuity floor. Each entry is a pointer one document
// makes into another part of the tree, and they are the reason the gate is worth
// running: if the scanner stops finding them it is judging a smaller corpus than
// it was written to judge, and the green above stops meaning anything. Follow a
// moved citation here; do not delete the entry.
var citationFloor = []citedPath{
	{doc: "AGENTS.md", token: "docs/quickstart.md"},
	{doc: "README.md", token: "docs/quickstart.md"},
	{doc: "docs/quickstart.md", token: ".formwork/formwork.yaml"},
	{doc: "docs/reference.md", token: "internal/rules/sqlparse/locking.go"},
}

// The rest of the floor is per-tree, for the reason publishedDocFloorDev
// records: this package crosses the cut, so it runs over the development tree
// AND over the tree the cut produces, and AGENTS.md/CLAUDE.md are different
// documents in each. In the development tree they are this repo's own agent
// instructions; in the published tree they are the overlays, materialised from
// tools/publication/public-*.md under those names.
//
// Both lists are non-empty deliberately. A cut-side floor of nothing would
// leave the published tree's run asserting only the four shared entries above
// while the overlay — the one document in that tree nobody edits directly —
// went unjudged.
var (
	citationFloorDev = []citedPath{
		{doc: "AGENTS.md", token: "internal/cli/introspect.go"},
		{doc: "CLAUDE.md", token: "internal/cli/cli.go"},
		{doc: "tools/publication/public-AGENTS.md", token: "docs/reference.md"},
	}
	citationFloorCut = []citedPath{
		{doc: "AGENTS.md", token: "docs/reference.md"},
	}
)
