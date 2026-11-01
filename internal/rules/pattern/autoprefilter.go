package pattern

import (
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// The engine runs a forbidden-pattern line by line over every in-scope file,
// and on a 4,300-rule corpus the regexp engine was more than half of a
// whole-tree check's CPU. Most files cannot match most patterns, and a literal
// gate says so for the price of one substring search over the file.
//
// autoPrefilter derives that gate from the pattern itself when the author
// declared none: literals at least one of which every match contains. A file
// whose content holds none of them cannot match, so skipping it changes no
// verdict. The gate runs on the same preprocessed variant the lines come
// from, and every line is a substring of that content.
//
// It returns nil, and the rule runs ungated, whenever it cannot name such a
// set: a non-RE2 syntax (regexp2's lookarounds are outside this analysis), a
// pattern that will not parse, an alternation branch with no guaranteed run,
// a case-folded literal (the gate is case-sensitive), a literal shorter than
// minAutoPrefilterLen, or one containing U+FFFD (Go's regexp matches invalid
// UTF-8 as U+FFFD, which a byte search would not find).
func autoPrefilter(src, syntaxName string) []string {
	if syntaxName != "" && syntaxName != "re2" {
		return nil
	}
	re, err := syntax.Parse(src, syntax.Perl)
	if err != nil {
		return nil
	}
	for re.Op == syntax.OpCapture {
		re = re.Sub[0]
	}
	branches := []*syntax.Regexp{re}
	if re.Op == syntax.OpAlternate {
		branches = re.Sub
	}
	if len(branches) > maxAutoPrefilterBranches {
		return nil
	}
	lits := make([]string, 0, len(branches))
	for _, b := range branches {
		runs, _ := guaranteedRuns(b)
		best := ""
		for _, r := range runs {
			if len(r) > len(best) {
				best = r
			}
		}
		if len(best) < minAutoPrefilterLen || strings.ContainsRune(best, utf8.RuneError) {
			return nil
		}
		lits = append(lits, best)
	}
	return lits
}

const (
	// minAutoPrefilterLen keeps the gate selective: a two-byte literal is in
	// most files, so it would cost a search and skip nothing.
	minAutoPrefilterLen = 3
	// maxAutoPrefilterBranches bounds the searches the gate may cost per file.
	maxAutoPrefilterBranches = 8
)

// containsAny reports whether s contains at least one of lits.
func containsAny(s string, lits []string) bool {
	for _, l := range lits {
		if strings.Contains(s, l) {
			return true
		}
	}
	return false
}
