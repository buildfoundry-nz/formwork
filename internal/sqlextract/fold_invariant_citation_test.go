// fold_invariant_citation_test.go — the spec's pointers at this package's
// source, checked against the source.
//
// The fold spec names one invariant of this package by quoting it and naming
// the file that states it: EMISSION ONLY EVER GROWS, in `fold.go`. That pointer
// is how a reader of §4.2 or §10 gets from "refusal was rejected" to the
// paragraph explaining why removing a world is the move nobody sees — the
// spec's own §10 says the rejected design "was the move `fold.go`'s EMISSION
// ONLY EVER GROWS exists to forbid, proposed by someone who had just quoted
// that comment into this spec", so the pointer failing is that mistake made
// again by a reader who could not find the comment.
//
// IT BROKE THE MOMENT THIS PACKAGE WAS SPLIT. fold.go reached the 750-line
// vendor cap, world enumeration moved to foldworlds.go, and the invariant went
// with it — the citation stayed. Nothing caught that: a path-resolution gate
// sees `fold.go`, which still exists, and every behavioural test in both
// packages is indifferent to which file a sentence lives in. A pointer that
// resolves to a real file holding none of what it promises is worse than a
// dangling one, because it reads as checked.
//
// So the citation is checked against the text. Every file this spec names
// beside the invariant has to be a file that states it, and the invariant has
// to be stated somewhere — a spec left citing a comment nobody kept fails here
// rather than at the next reader.
package sqlextract_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// foldSpecFromHere is the normative design spec, from this package's directory.
const foldSpecFromHere = "../../docs/specs/2026-07-29-sqlextract-assignment-flow-folding-design.md"

// foldInvariant is the one this package states in capitals and the spec quotes
// by name. Matched case-insensitively, because the spec quotes it lowercased in
// one place and shouting in the other.
const foldInvariant = "emission only ever grows"

// foldCitedGoFile matches a backticked Go source file — the form the spec uses
// when it points a reader at a specific comment.
var foldCitedGoFile = regexp.MustCompile("`([A-Za-z0-9_/.-]+\\.go)`")

// foldTrackedGo returns every tracked non-test Go file in the repository,
// keyed by base name, with the file's contents.
//
// TRACKED, VIA git ls-files, on census_wiring_test.go's reasoning: `make sync`
// materialises the validating targets under projects/ and they vendor this very
// package, so a filesystem walk finds a copy of fold.go and answers about a
// tree nobody builds. Non-test only, because a citation points a reader at the
// source that states an invariant, not at a test that quotes it.
func foldTrackedGo(t *testing.T) map[string][]string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	listed, err := exec.Command("git", "-C", root, "ls-files", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files failed, so the tracked file set is unknown — a run that "+
			"reported \"no file states this invariant\" from a file set it never read "+
			"would delete the spec's pointer on no evidence: %v", err)
	}
	out := map[string][]string{}
	read := 0
	for _, rel := range strings.Split(string(listed), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") ||
			strings.HasPrefix(rel, "examples/") {
			continue
		}
		read++
		b, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("reading tracked file %s: %v", rel, rerr)
		}
		base := filepath.Base(rel)
		if strings.Contains(strings.ToLower(string(b)), foldInvariant) {
			out[base] = append(out[base], rel)
		}
	}
	if read < 50 {
		t.Fatalf("the scan read %d tracked non-test Go file(s); this module has far "+
			"more, so a read that small is finding nothing rather than finding no "+
			"holder — which would condemn every citation whatever the tree says", read)
	}
	return out
}

// foldSentences splits the spec into sentences, so a citation is judged against
// the invariant it sits beside rather than against a paragraph that happens to
// mention both.
func foldSentences(spec string) []string {
	return strings.Split(strings.Join(strings.Fields(spec), " "), ". ")
}

// Every file the spec names beside this invariant states it.
func TestSpecCitesTheFileThatStatesTheEmissionInvariant(t *testing.T) {
	b, err := os.ReadFile(foldSpecFromHere)
	if err != nil {
		t.Fatalf("the fold design spec is the normative document for this package: %v", err)
	}
	holders := foldTrackedGo(t)
	if len(holders) == 0 {
		t.Fatalf("no tracked non-test Go file in this module states %q, and the spec "+
			"cites it as a comment a reader can go and read. Either the invariant was "+
			"deleted — in which case §4.2 and §10 are arguing from a rule this code no "+
			"longer keeps — or it was reworded, and the spec has to be reworded with it",
			foldInvariant)
	}

	cites := 0
	for _, s := range foldSentences(string(b)) {
		if !strings.Contains(strings.ToLower(s), foldInvariant) {
			continue
		}
		named := foldCitedGoFile.FindAllStringSubmatch(s, -1)
		if len(named) == 0 {
			continue
		}
		for _, m := range named {
			cites++
			base := filepath.Base(m[1])
			if _, ok := holders[base]; ok {
				continue
			}
			t.Errorf("the spec sends a reader to %s for %q, and no tracked non-test Go "+
				"file of that name states it. The files that do: %v.\n"+
				"A pointer that resolves to a real file holding none of what it promises "+
				"reads as checked and is not — this one broke when fold.go hit the "+
				"750-line vendor cap and world enumeration moved out from under the "+
				"citation. Sentence: %q", m[1], foldInvariant, foldHolderPaths(holders), s)
		}
	}
	if cites < 2 {
		t.Fatalf("the spec names a Go source file beside %q in %d sentence(s); §4.2 and "+
			"§10 each do, so a run finding fewer is matching prose that has been "+
			"rewritten out from under this test rather than checking it",
			foldInvariant, cites)
	}
}

// foldHolderPaths flattens the holder index for a failure message.
func foldHolderPaths(holders map[string][]string) []string {
	var out []string
	for _, paths := range holders {
		out = append(out, paths...)
	}
	return out
}
