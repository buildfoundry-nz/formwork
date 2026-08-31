package main

// inventory.go — the live-include-glob registry (#10876, #12398).
//
// EMPTY-GLOB catches a declared glob that matches nothing. It cannot catch a
// glob that was DELETED: once the line is gone from the rule, the per-glob arm
// has nothing to look at, and a thorough gaming run (delete the glob, delete
// its fire fixtures, trim the pass tree, comment out the subject) leaves the
// census, formwork check, and formwork test all green.
//
// The registry is the declared-sibling record that makes deletion leave a
// trace. Rows are keyed by rule (`rule-id<TAB>glob`) inside one packed file —
// tools/formwork-vacuity-census/live-include-globs.tsv — because one file per
// rule exploded the tracked tree past 2,000 inventories (#12398). A second
// rule landing adds its own rows and must not rewrite another rule's rows;
// write-inventory only replaces a rule's block when that rule's live set
// changed.
//
// Two gating verdicts keep it honest:
//
//   - GLOB-REMOVED   — the registry lists a glob the rule no longer declares
//                      (or the rule is gone). Cured by restoring the glob, or
//                      by removing the row in the same change.
//   - GLOB-UNTRACKED — a live include glob is declared with no row. Cured by
//                      adding the row (or by --write-inventory).
//
// Dead declared globs (EMPTY-GLOB / # glob-dead:) are not tracked. The
// registry is required when the corpus looks like a real guardrail tree
// (rule count above minCorpusForCanaries); synthetic fixtures skip it unless
// they plant a file.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WHERE THE BASELINE LIVES IS A PROPERTY OF THE CORPUS, NOT OF THIS TOOL.
//
// These were constants naming one repository's directory layout
// (tools/formwork-vacuity-census/...), which is the half of this census that
// could not be shared: the analysis is generic, the baseline is the adopting
// repo's own record of its own debt. Upstreaming the analysis without
// parameterising the path would have hard-coded one consumer's tree into the
// engine (buildfoundry-nz/formwork#5).
//
// The default sits beside the corpus it describes, under .formwork/. A repo
// that keeps it elsewhere passes --inventory; nothing here assumes a tools/
// directory exists.
var (
	// liveIncludeInventoryRel is the packed live-include registry (#12398).
	liveIncludeInventoryRel = filepath.Join(".formwork", "census", "live-include-globs.tsv")

	// liveIncludeInventoryDirRel is the retired per-rule directory.
	// Write-inventory deletes it if it is still present so a half-migrated tree
	// cannot serve both layouts.
	liveIncludeInventoryDirRel = filepath.Join(".formwork", "census", "live-include-globs")
)

// SetInventoryPath points the census at a corpus that keeps its registry
// somewhere other than the default. The retired directory is derived from it so
// the two can never disagree.
func SetInventoryPath(rel string) {
	liveIncludeInventoryRel = rel
	liveIncludeInventoryDirRel = strings.TrimSuffix(rel, filepath.Ext(rel))
}

// includeRegistryRel names the file an author edits. The pack is one file;
// the rule id is the first column, not the path.
func includeRegistryRel(ruleID string) string {
	_ = ruleID
	return liveIncludeInventoryRel
}

// inventoryPair is one (rule-id, glob) row.
type inventoryPair struct {
	ruleID string
	glob   string
}

func (p inventoryPair) key() string { return p.ruleID + "\t" + p.glob }

// loadLiveIncludeInventory reads the packed registry. The bool reports whether
// a registry exists at all. Malformed rows error.
func loadLiveIncludeInventory(root string) (map[inventoryPair]bool, bool, error) {
	abs := filepath.Join(root, filepath.FromSlash(liveIncludeInventoryRel))
	f, err := os.Open(abs)
	if os.IsNotExist(err) {
		return map[inventoryPair]bool{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	out := map[inventoryPair]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ruleID, glob, ok := strings.Cut(line, "\t")
		if !ok || ruleID == "" || glob == "" {
			return nil, true, fmt.Errorf("%s:%d: want rule-id<TAB>glob, got %q", liveIncludeInventoryRel, lineNo, line)
		}
		out[inventoryPair{ruleID: ruleID, glob: glob}] = true
	}
	if err := sc.Err(); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

// liveIncludePairs returns the set of (rule, glob) for every scope.include
// glob that currently matches at least one file.
func liveIncludePairs(gm *globMeasure) map[inventoryPair]bool {
	out := map[inventoryPair]bool{}
	for ruleID, globs := range gm.include {
		for _, g := range globs {
			if g.n > 0 {
				out[inventoryPair{ruleID: ruleID, glob: g.glob}] = true
			}
		}
	}
	return out
}

// declaredIncludePairs returns every declared scope.include (rule, glob),
// live or dead — the set GLOB-REMOVED is checked against.
func declaredIncludePairs(gm *globMeasure) map[inventoryPair]bool {
	out := map[inventoryPair]bool{}
	for ruleID, globs := range gm.include {
		for _, g := range globs {
			out[inventoryPair{ruleID: ruleID, glob: g.glob}] = true
		}
	}
	return out
}

// sortedInventoryPairs orders a slice of pairs for stable output.
func sortedInventoryPairs(in []inventoryPair) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].ruleID != in[j].ruleID {
			return in[i].ruleID < in[j].ruleID
		}
		return in[i].glob < in[j].glob
	})
}

// inventoryVerdicts compares the live-include registry to the measured globs.
func inventoryVerdicts(gm *globMeasure, inv map[inventoryPair]bool, present, required bool) map[string][]verdict {
	out := map[string][]verdict{}
	if !present {
		if required {
			out["formwork-live-include-inventory"] = []verdict{{
				class:  class1Glob,
				code:   "MISSING-INVENTORY",
				gating: true,
				why: fmt.Sprintf("%s is missing — that packed file is the declared-sibling record that makes live-glob deletion leave a trace (#10876, #12398). Generate it with: go run -C tools/formwork-vacuity-census . --write-inventory <repo-root>",
					liveIncludeInventoryRel),
			}}
		}
		return out
	}

	declared := declaredIncludePairs(gm)
	live := liveIncludePairs(gm)

	var removed []inventoryPair
	for p := range inv {
		if !declared[p] {
			removed = append(removed, p)
		}
	}
	sortedInventoryPairs(removed)
	for _, p := range removed {
		out[p.ruleID] = append(out[p.ruleID], verdict{
			class:  class1Glob,
			code:   "GLOB-REMOVED",
			gating: true,
			why: fmt.Sprintf("%s lists scope.include glob %q for rule %s but the rule no longer declares it — "+
				"deletion of a live glob is the gaming move the dead-glob arm cannot see. Restore the glob, or remove this "+
				"row in the same change (making the coverage loss reviewable) (#10876)", liveIncludeInventoryRel, p.glob, p.ruleID),
		})
	}

	var untracked []inventoryPair
	for p := range live {
		if !inv[p] {
			untracked = append(untracked, p)
		}
	}
	sortedInventoryPairs(untracked)
	for _, p := range untracked {
		out[p.ruleID] = append(out[p.ruleID], verdict{
			class:  class1Glob,
			code:   "GLOB-UNTRACKED",
			gating: true,
			why: fmt.Sprintf("live scope.include glob %q on rule %s is not in %s — add the row (or regenerate with --write-inventory) "+
				"so future deletion cannot go silent (#10876)", p.glob, p.ruleID, liveIncludeInventoryRel),
		})
	}
	return out
}

const includeRegistryHeader = "" +
	"# Packed live scope.include inventory (#10876, #12398).\n" +
	"# One row per live glob: rule-id<TAB>glob. The per-glob vacuity census\n" +
	"# fails if a row's glob is removed from its rule while still listed here\n" +
	"# (GLOB-REMOVED). Regenerate:\n" +
	"#   go run -C tools/formwork-vacuity-census . --write-inventory <repo-root>\n" +
	"# Only LIVE globs (match count > 0) are listed; dead declared globs are\n" +
	"# EMPTY-GLOB's job, and aspirational ones use `# glob-dead: <reason>`.\n"

// renderIncludeRegistry renders the packed registry from the full pair set.
func renderIncludeRegistry(pairs []inventoryPair) []byte {
	rows := append([]inventoryPair(nil), pairs...)
	sortedInventoryPairs(rows)
	var b strings.Builder
	b.WriteString(includeRegistryHeader)
	var last inventoryPair
	for i, p := range rows {
		if i > 0 && p == last {
			continue
		}
		last = p
		b.WriteString(p.ruleID)
		b.WriteByte('\t')
		b.WriteString(p.glob)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// rowsForRule extracts one rule's globs from a packed pair set, sorted.
func rowsForRule(pairs []inventoryPair, ruleID string) []string {
	var out []string
	for _, p := range pairs {
		if p.ruleID == ruleID {
			out = append(out, p.glob)
		}
	}
	sort.Strings(out)
	return out
}

// dedupeSorted drops adjacent repeats from an already-sorted slice.
func dedupeSorted(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// writeIfChanged writes body to dst only when what is there differs.
func writeIfChanged(dst string, body []byte) (bool, error) {
	old, err := os.ReadFile(dst)
	switch {
	case err == nil && bytes.Equal(old, body):
		return false, nil
	case err != nil && !os.IsNotExist(err):
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// writeLiveIncludeInventory rewrites the packed registry from the current
// live include pairs and removes the retired per-rule directory if it is
// still on disk.
func writeLiveIncludeInventory(root string, gm *globMeasure) (written, total int, err error) {
	var rows []inventoryPair
	for p := range liveIncludePairs(gm) {
		rows = append(rows, p)
	}
	total = 1
	dst := filepath.Join(root, filepath.FromSlash(liveIncludeInventoryRel))
	wrote, err := writeIfChanged(dst, renderIncludeRegistry(rows))
	if err != nil {
		return 0, total, err
	}
	if wrote {
		written = 1
	}
	if err := removeRetiredIncludeDir(root); err != nil {
		return written, total, err
	}
	return written, total, nil
}

func removeRetiredIncludeDir(root string) error {
	dir := filepath.Join(root, filepath.FromSlash(liveIncludeInventoryDirRel))
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(dir)
}
