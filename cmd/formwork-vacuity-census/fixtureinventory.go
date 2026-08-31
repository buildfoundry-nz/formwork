package main

// fixtureinventory.go — the fixture-directory registry (#10838, #12043).
//
// The class-3 arms judge the fixtures a rule still has: FIRE-FIXTURE-PASSES and
// PASS-FIXTURE-FIRES ask whether each surviving pair still discriminates,
// HALF-FIXTURE whether both directions are asserted, NO-FIXTURE whether there is
// any fixture at all. None of them can see a fixture that is GONE. Delete one
// directory from a rule with twelve and every instrument reports success:
// `formwork test` prints "OK — 11 fixture(s)", `formwork lint`'s
// [fixture-coverage] is satisfied by the survivors (its reverse arm, #10832,
// answers a rule that no longer exists, not a rule that lost one fixture), and
// the census stays green. That is the removal path for the by-construction
// guarantees the corpus rests on: a rule is weakened in two green commits —
// delete the fixture that pins the property, then make the change it forbade.
//
// The registry that closes it is PER RULE and lives INSIDE the tree it
// registers: .formwork/fixtures/<rule-id>/INVENTORY.tsv, one fixture directory
// name per line. Two gating verdicts keep it honest:
//
//   - FIXTURE-REMOVED   — the manifest lists a directory that is gone. The
//                         gaming move. Cured by restoring the fixture, or by
//                         dropping the row in the SAME change, which is what
//                         puts the coverage loss in the diff a reviewer reads.
//   - FIXTURE-UNTRACKED — a fixture directory exists with no row. Cured by
//                         adding the row (or by --write-inventory). Without it
//                         the registry would only be as complete as the last
//                         person remembered to make it, and tomorrow's deletion
//                         would have nothing to be missing from.
//
// #12043 is why it is per rule rather than one repo-global TSV. A single file
// derived from the whole tree has its content rewritten by every rule that
// lands, so landing one fixture-bearing rule on develop reached into every other
// open PR's copy: the author met a gate naming directories belonging to a rule
// they had never seen, and the cure had them author rows for someone else's
// work. Keyed by rule, the file a rule's change writes is a file no other
// change touches. Co-location does the rest: the rule id is the parent
// directory, so a manifest cannot name another rule's fixtures, and the manifest
// travels with the tree — including an orphaned tree whose rule is gone, which
// is [fixture-coverage]'s FAIL (#10832) and cannot sit here quietly.
//
// `<dir>.want` manifests are files beside the directory, not directories, so
// they are not rows — deleting one already turns `formwork test` red, because a
// fire fixture's findings must match its declared expectations exactly. The
// engine ignores every non-directory entry inside a rule's fixture tree
// (internal/fixturetest/run.go, internal/meta/lint.go), which is what lets the
// manifest live there at all.
//
// Like the live-include registry, it is required only when the corpus looks like
// a real guardrail tree (rule count above minCorpusForCanaries); synthetic
// fixtures skip it unless they plant a manifest.

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// fixturesRootRel is the repo-relative root of the fixture trees.
const fixturesRootRel = ".formwork/fixtures"

// fixtureManifestName is the per-rule registry file inside each fixture tree.
const fixtureManifestName = "INVENTORY.tsv"

// fixtureInventoryRowID is the synthetic row MISSING-FIXTURE-INVENTORY is
// attached to, so report() has somewhere to print it when no rule is at fault.
const fixtureInventoryRowID = "formwork-fixture-inventory"

// fixtureManifestRel is a rule's manifest path, for messages that must name the
// one file an author has to edit.
func fixtureManifestRel(ruleID string) string {
	return path.Join(fixturesRootRel, ruleID, fixtureManifestName)
}

// fixtureDir is one (rule-id, fixture-directory) row.
type fixtureDir struct {
	ruleID string
	dir    string
}

// loadFixtureInventory reads every per-rule manifest. The bool reports whether
// the corpus carries a registry AT ALL, so the caller can decide whether total
// absence is fatal; a corpus carrying some manifests and not others is present,
// and the rules without one answer for their directories through
// FIXTURE-UNTRACKED. Malformed rows error.
func loadFixtureInventory(root string) (map[fixtureDir]bool, bool, error) {
	out := map[fixtureDir]bool{}
	rules, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(fixturesRootRel)))
	if os.IsNotExist(err) {
		return out, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	present := false
	for _, r := range rules {
		if !r.IsDir() {
			continue
		}
		rows, found, err := readFixtureManifest(root, r.Name())
		if err != nil {
			return nil, true, err
		}
		if !found {
			continue
		}
		present = true
		for _, dir := range rows {
			out[fixtureDir{ruleID: r.Name(), dir: dir}] = true
		}
	}
	return out, present, nil
}

// readFixtureManifest reads one rule's manifest. Each row is a bare directory
// name: the rule id is the parent directory, so there is nothing for a row to
// contradict. A row carrying a tab or a path separator is rejected rather than
// silently reinterpreted — that is the shape of the old repo-global
// "rule-id<TAB>dir" format, and reading one as a directory name would make the
// registry quietly track a directory that cannot exist.
func readFixtureManifest(root, ruleID string) ([]string, bool, error) {
	rel := fixtureManifestRel(ruleID)
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, "\t/\\") {
			return nil, true, fmt.Errorf("%s:%d: want a bare fixture directory name, got %q — the rule id is this file's parent directory, not part of the row", rel, lineNo, line)
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

// onDiskFixtureDirs is every fire-*/pass-* directory under .formwork/fixtures/.
// A subdirectory with neither prefix is skipped rather than rejected: the engine
// itself already fails on one belonging to a live rule (classify.go), and this
// walk must stay a measurement of what is there.
func onDiskFixtureDirs(root string) (map[fixtureDir]bool, error) {
	out := map[fixtureDir]bool{}
	fixturesRoot := filepath.Join(root, filepath.FromSlash(fixturesRootRel))
	rules, err := os.ReadDir(fixturesRoot)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if !r.IsDir() {
			continue
		}
		dirs, err := os.ReadDir(filepath.Join(fixturesRoot, r.Name()))
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			if !strings.HasPrefix(d.Name(), "fire-") && !strings.HasPrefix(d.Name(), "pass-") {
				continue
			}
			out[fixtureDir{ruleID: r.Name(), dir: d.Name()}] = true
		}
	}
	return out, nil
}

// sortedFixtureDirs orders a set for stable output.
func sortedFixtureDirs(set map[fixtureDir]bool) []fixtureDir {
	out := make([]fixtureDir, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ruleID != out[j].ruleID {
			return out[i].ruleID < out[j].ruleID
		}
		return out[i].dir < out[j].dir
	})
	return out
}

// fixtureInventoryVerdicts compares the registry to the directories on disk.
//
//	present == false && required → MISSING-FIXTURE-INVENTORY (one synthetic row)
//	present == false && !required → nil (synths without a registry)
//	present == true → FIXTURE-REMOVED / FIXTURE-UNTRACKED as needed
func fixtureInventoryVerdicts(onDisk, inv map[fixtureDir]bool, present, required bool) map[string][]verdict {
	out := map[string][]verdict{}
	if !present {
		if required {
			out[fixtureInventoryRowID] = []verdict{{
				class:  class3,
				code:   "MISSING-FIXTURE-INVENTORY",
				gating: true,
				why: fmt.Sprintf("no rule carries a %s — that per-rule manifest is the registry that makes deleting a "+
					"fixture directory leave a trace (#10838). Generate them with: go run -C tools/formwork-vacuity-census . --write-inventory <repo-root>",
					fixtureManifestName),
			}}
		}
		return out
	}

	for _, p := range sortedFixtureDirs(inv) {
		if onDisk[p] {
			continue
		}
		out[p.ruleID] = append(out[p.ruleID], verdict{
			class:  class3,
			code:   "FIXTURE-REMOVED",
			gating: true,
			why: fmt.Sprintf("%s lists %s but that directory is gone — the surviving fixtures still behave, so no "+
				"class-3 arm can see the loss. Restore the fixture, or drop this row in the same change so the "+
				"coverage loss is reviewable (#10838)", fixtureManifestRel(p.ruleID), p.dir),
		})
	}

	for _, p := range sortedFixtureDirs(onDisk) {
		if inv[p] {
			continue
		}
		out[p.ruleID] = append(out[p.ruleID], verdict{
			class:  class3,
			code:   "FIXTURE-UNTRACKED",
			gating: true,
			why: fmt.Sprintf("fixture directory %s/%s/%s is not in %s — add the row (or regenerate with "+
				"--write-inventory) so its deletion cannot go silent (#10838). That manifest belongs to this rule "+
				"alone, so the finding is about a fixture in your own change, never one another PR landed (#12043)",
				fixturesRootRel, p.ruleID, p.dir, fixtureManifestRel(p.ruleID)),
		})
	}
	return out
}

// fixtureManifestHeader is the comment block every fixture manifest carries, held
// by the renderer for the same reason as its live-include sibling.
const fixtureManifestHeader = "" +
	"# fixture directories for this rule — the vacuity census fails if a row's directory\n" +
	"# is deleted while the row is still listed (FIXTURE-REMOVED, #10838), or if a\n" +
	"# directory exists with no row (FIXTURE-UNTRACKED).\n" +
	"# One directory name per line; the rule id is this file's parent directory, so no\n" +
	"# other rule's change ever writes this file (#12043). Regenerate:\n" +
	"#   go run -C tools/formwork-vacuity-census . --write-inventory <repo-root>\n" +
	"# Membership is a FLOOR, not a health claim: whether each fixture still\n" +
	"# discriminates is the class-3 behavioural arms' question, and a fixture tree that\n" +
	"# never runs is #10968's. Removing a row is a reviewed deletion, never a cleanup.\n"

// renderFixtureManifest renders one rule's manifest — header block and rows — as
// the exact bytes that belong on disk, the fixture-tree half of what
// renderIncludeRegistry does for the live-include registries (#13521). This side
// had drifted further: eight committed manifests carried no header at all.
// Sorts and de-duplicates its own rows, for the reasons renderIncludeRegistry
// gives: the conformance test round-trips a committed file through its own parsed
// rows, so ordering and uniqueness have to be decided here or neither gets pinned.
func renderFixtureManifest(dirs []string) []byte {
	rows := append([]string(nil), dirs...)
	sort.Strings(rows)
	rows = dedupeSorted(rows)

	var b strings.Builder
	b.WriteString(fixtureManifestHeader)
	for _, d := range rows {
		b.WriteString(d)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// writeFixtureInventory rewrites every per-rule manifest from the directories on
// disk, and reports how many it actually WROTE. A rule that has lost its whole
// tree loses its manifest with it — the same erasure the repo-global regeneration
// performed on its rows, and the reason regeneration is an author's deliberate act
// rather than something CI does for them. Used by --write-inventory.
//
// Skips a manifest whose rendered bytes already match, the same contract as the
// live-include registries (#13521). This side had drifted further: eight committed
// manifests carried no header block at all.
func writeFixtureInventory(root string, onDisk map[fixtureDir]bool) (written, total int, err error) {
	byRule := map[string][]string{}
	for _, p := range sortedFixtureDirs(onDisk) {
		byRule[p.ruleID] = append(byRule[p.ruleID], p.dir)
	}

	fixturesRoot := filepath.Join(root, filepath.FromSlash(fixturesRootRel))
	rules, err := os.ReadDir(fixturesRoot)
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, err
	}
	for _, r := range rules {
		if !r.IsDir() || len(byRule[r.Name()]) > 0 {
			continue
		}
		if err := os.Remove(filepath.Join(fixturesRoot, r.Name(), fixtureManifestName)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return written, total, err
		}
		total++
		written++
	}

	for ruleID, dirs := range byRule {
		total++
		dst := filepath.Join(fixturesRoot, ruleID, fixtureManifestName)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return written, total, err
		}
		wrote, err := writeIfChanged(dst, renderFixtureManifest(dirs))
		if err != nil {
			return written, total, err
		}
		if wrote {
			written++
		}
	}
	return written, total, nil
}
