// Package census holds the shape and the bookkeeping every rule-corpus census
// shares.
//
// Several censuses — formwork-universal-cure-census and
// formwork-subject-census among them — ask different questions of
// .formwork/rules/*.yaml and then do the IDENTICAL thing with the answer:
// report every flagged arm and fail if there is one. Detection is the part that
// differs and stays in each tool; this is the part that does not.
//
// There is no known-debt list. The helper once read one, a FILE:ARM-ID
// allowlist that carried a flagged arm as a pass; a flagged arm is now cured or
// the census fails.
package census

import (
	"fmt"
	"io"
)

// Finding is one flagged rule arm: the repo-relative rule file, the 1-based
// line the verdict anchors to, and the arm's id for the report.
type Finding struct {
	File string
	Line int
	Arm  string
}

// Reconcile writes the verdict for one census run and returns the problem
// count: every flagged arm is a problem. why is the one-line explanation
// appended to each report, so the failing arm carries its reason without the
// caller re-templating the whole line. A zero return is the only pass.
func Reconcile(w io.Writer, ruleID string, flags []Finding, why string) int {
	files := map[string]bool{}
	for _, fl := range flags {
		files[fl.File] = true
		fmt.Fprintf(w, "NEW %s:%d: arm %q %s\n", fl.File, fl.Line, fl.Arm, why)
	}
	if len(flags) > 0 {
		fmt.Fprintf(w, "%s: %d problem(s); %d flagged arm(s) in %d file(s)\n", ruleID, len(flags), len(flags), len(files))
		return len(flags)
	}
	fmt.Fprintf(w, "%s: OK — 0 flagged arm(s)\n", ruleID)
	return 0
}
