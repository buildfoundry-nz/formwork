// Package census holds the shape and the shrink-only bookkeeping every
// rule-corpus census shares.
//
// Two censuses — formwork-universal-cure-census and formwork-anchor-census —
// ask different questions of .formwork/rules/*.yaml and then do the IDENTICAL
// thing with the answer: key each flagged arm as FILE:ARM-ID, reconcile that
// against a per-arm known-debt list, and fail on a new offender OR on a stale
// entry. Detection is the part that differs and stays in each tool; this is
// the part that does not.
package census

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Finding is one flagged rule arm: the repo-relative rule file, the 1-based
// line the verdict anchors to, and the arm's id for the report.
type Finding struct {
	File string
	Line int
	Arm  string
}

// Key is the known-debt key for a finding.
//
// Per ARM, not per FILE. A file-keyed list waves through a NEW offending arm
// added to any listed file, which is the hole the ratchets exist to close; it
// also leaves an entry live after one arm of a two-arm file is cured, so
// nothing forces the entry out.
func (f Finding) Key() string { return f.File + ":" + f.Arm }

// ReadDebtList loads a known-debt allowlist: one FILE:ARM-ID key per line,
// `#` comments and blanks ignored.
//
// A missing file is returned as an error, never as an empty set: deleting the
// allowlist would otherwise read as "no debt" and silently pass every arm that
// was being carried.
func ReadDebtList(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	set := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = true
	}
	return set, sc.Err()
}

// Reconcile writes the verdict for one census run and returns the problem
// count: new offenders plus stale entries. why is the one-line explanation
// appended to each NEW report, so the failing arm carries its reason without
// the caller re-templating the whole line.
//
// A zero return is the only pass. Carried debt is printed but not counted;
// a stale entry IS counted, which is what makes the list shrink-only.
func Reconcile(w io.Writer, ruleID string, flags []Finding, debt map[string]bool, why string) int {
	var problems, carried int
	flagged := map[string]bool{}
	files := map[string]bool{}
	for _, fl := range flags {
		files[fl.File] = true
		flagged[fl.Key()] = true
		if debt[fl.Key()] {
			carried++
			fmt.Fprintf(w, "  known debt: %s:%d (%s)\n", fl.File, fl.Line, fl.Arm)
			continue
		}
		problems++
		fmt.Fprintf(w, "NEW %s:%d: arm %q %s\n", fl.File, fl.Line, fl.Arm, why)
	}
	// Sorted, so a CI log diff between two runs is meaningful rather than
	// reshuffled by map iteration order.
	stale := make([]string, 0, len(debt))
	for entry := range debt {
		if !flagged[entry] {
			stale = append(stale, entry)
		}
	}
	sort.Strings(stale)
	for _, entry := range stale {
		problems++
		fmt.Fprintf(w, "STALE %s: allowlisted but no longer trips the detector — cure the arm or delete the entry (self-cleaning list)\n", entry)
	}

	if problems > 0 {
		fmt.Fprintf(w, "%s: %d problem(s); %d flagged arm(s) in %d file(s), %d carried as known debt\n",
			ruleID, problems, len(flags), len(files), carried)
		return problems
	}
	fmt.Fprintf(w, "%s: OK — %d flagged arm(s), all %d known-debt entries live\n", ruleID, len(flags), len(debt))
	return 0
}
