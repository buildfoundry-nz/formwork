package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// maxEvidenceLines bounds the localisation walk: three load-bearing lines are
// enough to show a human what the rule is actually matching.
const maxEvidenceLines = 3

// diffuseThreshold is how many independently sufficient lines make a witness's
// evidence diffuse. Below it, deleting one line trips the rule; at or above it,
// no single regression can.
const diffuseThreshold = 12

// satisfies reports whether r passes over a tree holding only f. The verdict
// comes from engine.Run, never from a re-implementation of the matcher: a rule
// carries per-run state (required-pattern exists mode accumulates across files),
// so each probe gets a Fresh checker.
func satisfies(r *config.Rule, root string, f *scan.File) bool {
	fresh, err := r.Fresh()
	if err != nil {
		return false
	}
	fresh.Allowlist = nil
	fds, err := engine.Run([]*config.Rule{fresh}, &scan.FileSet{Root: root, Files: []*scan.File{f}}, 1)
	if err != nil {
		return false
	}
	return len(finding.Unsuppressed(fds)) == 0
}

// exceptExcuses reports whether r would actually fire on f — that is, whether
// the except.paths entry naming f suppresses anything real (#10777).
//
// Clearing ExceptPaths is the whole point, not a detail. except.paths is a
// scope SUBTRACTION: config.Rule.Applies returns false for a matched path, so
// engine.Run drops f before any checker sees it. Probe with the rule as written
// and every entry in the corpus reports "the rule does not fire here", the arm
// gates the whole corpus, and its own test passes while measuring nothing.
// Allowlist is cleared for the same reason satisfies does it — the question is
// what THIS rule does to THIS file, not what a second exemption channel would
// then do about it, and dropping it can only produce more findings, which is
// generous to the author.
//
// SUPPRESSED findings count as firing. The question is whether the rule reaches
// f at all, and a marker- or allowlist-suppressed finding proves it does; that
// is also how formwork lint's own exemption-hygiene reads its allowlist
// staleness ("suppressed findings count as still trips"). An error is returned
// rather than folded into a verdict: a probe that could not run must never read
// as "the exception excuses nothing".
func exceptExcuses(r *config.Rule, root string, f *scan.File) (bool, error) {
	fresh, err := r.Fresh()
	if err != nil {
		return false, err
	}
	fresh.Allowlist = nil
	fresh.ExceptPaths = nil
	fds, err := engine.Run([]*config.Rule{fresh}, &scan.FileSet{Root: root, Files: []*scan.File{f}}, 1)
	if err != nil {
		return false, err
	}
	return len(fds) > 0, nil
}

// satisfiesSet reports whether r passes over a WHOLE file set. The relation
// probes need this grain: a set-relation or pair-consistency verdict is a
// property of the corpus, never of one file, so it cannot be read off
// satisfies() above. Same discipline — a Fresh checker, engine.Run, never a
// re-implementation of the matcher.
func satisfiesSet(r *config.Rule, root string, fs []*scan.File) bool {
	fresh, err := r.Fresh()
	if err != nil {
		return false
	}
	fresh.Allowlist = nil
	fds, err := engine.Run([]*config.Rule{fresh}, &scan.FileSet{Root: root, Files: fs}, 1)
	if err != nil {
		return false
	}
	return len(finding.Unsuppressed(fds)) == 0
}

// witnesses returns the in-scope files that satisfy r on their own — for an
// existence obligation, the files where the evidence lives.
func witnesses(r *config.Rule, root string, inScope []*scan.File) ([]*scan.File, error) {
	var out []*scan.File
	for _, f := range inScope {
		if _, err := f.Content(); err != nil {
			return nil, fmt.Errorf("rule %s: reading %s: %w", r.ID, f.Path(), err)
		}
		if satisfies(r, root, f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// blank returns an in-memory twin of f with the 1-based lines in off emptied.
// Line count is preserved so reported line numbers stay the file's own.
func blank(f *scan.File, lines []string, off map[int]bool) *scan.File {
	cp := make([]string, len(lines))
	for i, l := range lines {
		if !off[i+1] {
			cp[i] = l
		}
	}
	return scan.NewMemFile(f.Path(), []byte(strings.Join(cp, "\n")+"\n"))
}

// commentPlane returns f with every NON-comment line emptied — the comment
// plane alone — and whether the file has any comment at all. A rule that still
// passes on this is a rule whose subject could be deleted in full while the
// prose about it kept the gate green.
func commentPlane(f *scan.File) (*scan.File, bool) {
	lines, err := f.Lines()
	if err != nil {
		return f, false
	}
	off, comments := map[int]bool{}, 0
	for i, l := range lines {
		if isComment(f.Path(), l) {
			comments++
		} else {
			off[i+1] = true
		}
	}
	if comments == 0 {
		return f, false
	}
	return blank(f, lines, off), true
}

// codePlane returns f with every comment-only line emptied — the code plane
// alone. A COMMENT-SUFFICIENT rule that ALSO passes here has real backing today
// and can be tightened without changing its verdict; one that does not is a rule
// with nothing behind it at all, and tightening it will correctly turn red.
func codePlane(f *scan.File) *scan.File {
	lines, err := f.Lines()
	if err != nil {
		return f
	}
	off := map[int]bool{}
	for i, l := range lines {
		if isComment(f.Path(), l) {
			off[i+1] = true
		}
	}
	return blank(f, lines, off)
}

// commentLines returns the comment-only lines of f that match nothing in
// particular — rendered as evidence, capped, so a COMMENT-SUFFICIENT verdict
// can be checked by eye.
func commentLines(r *config.Rule, root string, f *scan.File) []string {
	lines, err := f.Lines()
	if err != nil {
		return nil
	}
	// Localise inside the comment plane: which comment lines actually carry it.
	plane, ok := commentPlane(f)
	if !ok {
		return nil
	}
	var out []string
	for _, ln := range loadBearing(r, root, plane, maxEvidenceLines) {
		txt := ""
		if ln-1 < len(lines) {
			txt = strings.TrimSpace(lines[ln-1])
		}
		if len(txt) > 110 {
			txt = txt[:110] + "…"
		}
		out = append(out, fmt.Sprintf("%s:%d: %s", f.Path(), ln, txt))
	}
	return out
}

// loadBearing returns up to cap 1-based line numbers in witness f whose removal
// costs r its pass. Each line is found by binary search over "blank lines 1..k
// and see whether evidence survives", so a 10 000-line file costs ~14 probes per
// line rather than 10 000.
func loadBearing(r *config.Rule, root string, f *scan.File, cap int) []int {
	lines, err := f.Lines()
	if err != nil || len(lines) == 0 {
		return nil
	}
	off := map[int]bool{}
	var out []int
	for len(out) < cap {
		if !satisfies(r, root, blank(f, lines, off)) {
			break // no evidence left to localise
		}
		lo, hi := 1, len(lines)
		for lo < hi {
			mid := (lo + hi) / 2
			trial := map[int]bool{}
			for k := range off {
				trial[k] = true
			}
			for i := 1; i <= mid; i++ {
				trial[i] = true
			}
			if satisfies(r, root, blank(f, lines, trial)) {
				lo = mid + 1 // evidence survives past mid
			} else {
				hi = mid // evidence was inside [lo, mid]
			}
		}
		if lo > len(lines) || off[lo] {
			break
		}
		out = append(out, lo)
		off[lo] = true
	}
	sort.Ints(out)
	return out
}

// localiseAll renders the load-bearing lines of every witness as evidence.
func localiseAll(r *config.Rule, root string, ws []*scan.File) []string {
	var out []string
	for _, f := range ws {
		lines, err := f.Lines()
		if err != nil {
			continue
		}
		for _, ln := range loadBearing(r, root, f, maxEvidenceLines) {
			txt := ""
			if ln-1 < len(lines) {
				txt = strings.TrimSpace(lines[ln-1])
			}
			if len(txt) > 110 {
				txt = txt[:110] + "…"
			}
			out = append(out, fmt.Sprintf("%s:%d: %s", f.Path(), ln, txt))
		}
	}
	return out
}

// isComment reports whether a line is comment-only for its file's language.
// Deliberately conservative — a line that merely CONTAINS a comment is not one,
// so a mixed code+comment line is never blanked and never mistaken for prose.
func isComment(path, line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	switch {
	case hasAnySuffix(path, ".go", ".dart", ".proto"):
		return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*")
	case hasAnySuffix(path, ".sql"):
		return strings.HasPrefix(t, "--")
	case hasAnySuffix(path, ".sh", ".bash", ".yml", ".yaml", ".tsv", ".txt", ".toml", ".cfg"):
		return strings.HasPrefix(t, "#")
	}
	return false
}

// curableLang reports whether formwork carries a decomment-* projection for
// this file's language — i.e. whether a COMMENT-SUFFICIENT verdict is something
// the rule's author can actually ACT on by declaring a preprocess. Today only
// Go has one. Without it the only ways to answer the verdict are to hand-anchor
// the pattern (which breaks the deliberate comment-borne marker vocabularies
// this repo uses, e.g. `# INTEGRATION_RUN` in ci.yml) or to leave the rule
// alone. A gate whose cure does not exist is a gate that punishes the author
// for the engine's gap, so those verdicts are measured, not gated.
func curableLang(path string) bool {
	return hasAnySuffix(path, ".go")
}

func hasAnySuffix(s string, sfx ...string) bool {
	for _, x := range sfx {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}
