package main

// existsbirth.go — the birth ratchet on `mode: exists` (#15848).
//
// `mode: exists` asks whether the pattern appears ANYWHERE in scope. Over one
// file that is the same question as the default per-file mode. Over two hundred,
// one incidental hit satisfies the obligation for all of them, and deleting the
// real thing is masked by a coincidence in a file nobody was thinking about.
//
// # Why birth only
//
// The retrospective form was measured and correctly abandoned. The only
// predicate with full recall over the known instances fires 63 times, and the
// two gateable sub-shapes are disjoint because one is a per-unit obligation and
// the other a global one — a distinction of INTENT, invisible in the text. No
// gate can sort the standing set. The standing arms are excluded by
// construction here: they are never in the diff, so there is no grandfather
// list, no baseline file, and therefore no regenerator that could silently grow
// one. The exemption IS the diff.
//
// # Why the predicate reads text only
//
// The measured recommendation was the union of "more than one file matched" and
// "at least one wildcard glob". The first half is a property of the rule AND the
// tree. This arm judges a TRANSITION between two commits, so a rule whose scope
// drifts from one file to two with nobody editing it would fire on whoever
// pushed next — collateral on an author who did nothing. `declared-glob-count >
// 1` keeps the coverage with no tree dependency: it catches the two-literal-globs
// spelling that no wildcard test sees, while the wildcard half catches the arms
// matching one file today that go vacuous the moment a second lands. Those are
// invisible to a file-count predicate at birth, which is the whole reason to
// gate at birth rather than on the tree.
//
// # The declaration is a REVIEWED escape, and this gate does not validate it
//
// A material part of the standing set is existential BY DESIGN — "X has a live
// caller", "X has a consumer somewhere in the frontend". The exact share is NOT
// quantified here on purpose: the only figure anyone has produced for it came
// from a source nobody has independently checked, and intent is not mechanically
// derivable from the rule text, which is the same reason no retrospective gate
// exists. The class is real and load-bearing; the fraction is unmeasured. For
// those arms the
// multi-file scope IS the invariant and narrowing it would assert something the
// author does not mean; a gate that refuses them outright is one that gets
// switched off. And nothing in the text distinguishes a per-unit obligation from
// a global one at birth — the same reason no retrospective gate exists. So the
// gate checks only that a human WROTE a reason. It cannot check that the reason
// is a good one, and it says so rather than implying otherwise.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// existsDeclarations reports which rule ids carry the reviewed escape, read from
// the rule files as TEXT because the marker is a comment and exists nowhere
// else — the same reason `# glob-dead:` and `# except-declaration:` are read
// this way.
//
// It accepts the marker anywhere in the CONTIGUOUS comment block immediately
// above the arm's `- id:` line, not solely on the line directly above. The two
// per-entry markers sit above a single glob scalar where one line is enough; a
// reason for an arm's whole scope is a sentence about intent and wraps in
// practice, and a convention that silently stops working at the line break is
// one that reads as absent exactly when someone took the trouble to explain
// themselves.
//
// A bare marker with no reason declares nothing, matching `# glob-dead:`.
func existsDeclarations(root string) (map[string]bool, error) {
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			m := ruleIDDecl.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			for j := i - 1; j >= 0; j-- {
				above := strings.TrimSpace(lines[j])
				if !strings.HasPrefix(above, "#") {
					break
				}
				if rest, ok := strings.CutPrefix(above, existsDeclaration); ok {
					if strings.TrimSpace(rest) != "" {
						out[m[1]] = true
					}
					break
				}
			}
		}
	}
	return out, nil
}

// existsDeclaration is the in-place reviewed escape, matching the `# glob-dead:`
// and `# except-declaration:` conventions this census already reads. A bare
// marker with no reason declares nothing, the same non-empty-reason rule
// `# glob-dead:` uses.
const existsDeclaration = "# exists-multi-file:"

// existsBirthReason says WHY a required-pattern arm combining `mode: exists`
// with a scope wider than one file is refused at birth, or "" when it is not.
//
// The predicate reads rule TEXT only — the mode, the declared globs, and whether
// a reviewed declaration is present. It never counts files matched in the tree,
// because this arm judges a TRANSITION between two commits and a rule whose
// scope drifts from one file to two with no edit by anyone would otherwise fire
// on whoever pushed next.
func existsBirthReason(mode string, globs []string, declared bool) string {
	if mode != "exists" || declared {
		return ""
	}
	wild := 0
	for _, g := range globs {
		if strings.ContainsAny(g, "*?[") {
			wild++
		}
	}
	switch {
	case wild > 0:
		return fmt.Sprintf("mode: exists over a WILDCARD scope (%d of %d globs: %s). The obligation is "+
			"satisfied by ONE incidental match anywhere the glob reaches, so deleting the thing this rule "+
			"guards is masked by a coincidence in a file nobody was thinking about — and a scope matching "+
			"one file today goes vacuous the moment a second lands, with nothing re-asking",
			wild, len(globs), strings.Join(globs, ", "))
	case len(globs) > 1:
		return fmt.Sprintf("mode: exists over %d declared globs (%s). One of those files carrying the "+
			"pattern satisfies the rule for all of them, so the rule cannot tell which file was supposed "+
			"to carry it", len(globs), strings.Join(globs, ", "))
	}
	return ""
}

const existsBirthCure = "cure: narrow scope.include onto the file that actually carries the evidence — " +
	"over a single file `mode: exists` and the default per-file mode are the same question, so the arm " +
	"keeps its meaning and gains the ability to fail. If the obligation really is per-unit, DROP " +
	"`mode: exists` and let the default oblige every file in scope. Do NOT reach for " +
	"`replace-literal-in-scope` to prove it instead: that spec kind requires two or more declared sites " +
	"and a newborn arm has one, so the proof is refused before it runs. If the rule genuinely means " +
	"\"this exists SOMEWHERE\" — a live caller, a consumer anywhere in the frontend — say so in place " +
	"above the arm's id with `" + existsDeclaration + " <reason>`. That is a reviewed escape: this gate " +
	"checks only that a reason was written, never that it is a good one."
