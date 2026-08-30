// Package report renders findings. The human format preserves the verdict
// contract downstream tooling and habits depend on: [rule-id] OK/FAIL lines
// and Cure blocks (spec §6).
package report

import (
	"fmt"
	"io"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
)

// Human writes the per-rule verdicts and summary. rls must be in the order
// to display (config.Load returns them ID-sorted); findings may be in any
// order but are conventionally pre-sorted by the engine, and may include
// Suppressed entries (spec §5 exemptions) — Human owns filtering those out
// of the per-rule verdicts and finding tally itself (report owns all
// formatting).
//
// SUPPRESSED FINDINGS ARE ENUMERATED, NOT COUNTED (#57). Each one gets a line
// naming its rule, location, message and the channel that exempted it
// ("marker", "allowlist:<file>:<line>") — the same facts JSON and GitHub carry
// (#91) — under a heading that says in words that these are exemptions rather
// than failures. A bare integer told a reader that something was exempted and
// never what, so the surface could not be audited or burned down from `check`
// output at all; downstream, `1335/1335 rules passed, 0 findings, 14
// suppressed` was quoted as a clean bill by several readers who had no
// affordance to ask which fourteen.
//
// The summary's count is len(that enumeration), deliberately: computing it
// independently as len(findings)-len(live) is what let a count exist with no
// list beside it to disagree with. One source, so they cannot drift.
//
// The scan block sits between the per-rule verdicts and the summary line: the
// verdicts are what the run found, the block is what it looked at to find it,
// and the summary line stays last because habits and downstream tooling read it
// there. The suppressed block sits directly above that summary line — as close
// as possible to the number it produces.
func Human(w io.Writer, rls []*config.Rule, findings []finding.Finding, scan ScanSummary) {
	live := finding.Unsuppressed(findings)

	byRule := map[string][]finding.Finding{}
	for _, f := range live {
		byRule[f.RuleID] = append(byRule[f.RuleID], f)
	}
	passed := 0
	for _, r := range rls {
		fs := byRule[r.ID]
		if len(fs) == 0 {
			passed++
			fmt.Fprintf(w, "[%s] OK\n", r.ID)
			continue
		}
		verdict := "WARN"
		if hasError(fs) {
			verdict = "FAIL"
		} else {
			passed++
		}
		fmt.Fprintf(w, "[%s] %s — %d finding(s)\n", r.ID, verdict, len(fs))
		for _, f := range fs {
			fmt.Fprintf(w, "  %s\n", location(f))
		}
		if r.Cure != "" {
			fmt.Fprintf(w, "  Cure: %s\n", r.Cure)
		}
	}
	fmt.Fprintf(w, "scan: %s\n", scan.headline())
	for _, l := range scan.details() {
		fmt.Fprintf(w, "  %s\n", l)
	}
	suppressed := suppressedLines(findings)
	if len(suppressed) > 0 {
		fmt.Fprintln(w, "suppressed (exempted, not failures):")
		for _, l := range suppressed {
			fmt.Fprintf(w, "  %s\n", l)
		}
		fmt.Fprintf(w, "formwork: %d/%d rules passed, %d finding(s), %d suppressed\n", passed, len(rls), len(live), len(suppressed))
	} else {
		fmt.Fprintf(w, "formwork: %d/%d rules passed, %d finding(s)\n", passed, len(rls), len(live))
	}
}

// suppressedLines renders one line per suppressed finding, in the order the
// engine sorted them (rule id, path, line), and is the ONLY place Human learns
// how many there were — the summary count is its length. Every suppressed
// finding produces a line: a rule- or scope-level one with no path still gets
// named, because "we exempted something and cannot tell you where" is the
// answer this change exists to remove.
func suppressedLines(findings []finding.Finding) []string {
	var out []string
	for _, f := range findings {
		if !f.Suppressed {
			continue
		}
		out = append(out, fmt.Sprintf("[%s] %s (%s)", f.RuleID, location(f), f.SuppressedBy))
	}
	return out
}

func hasError(fs []finding.Finding) bool {
	for _, f := range fs {
		if f.Severity == finding.SeverityError {
			return true
		}
	}
	return false
}

func location(f finding.Finding) string {
	switch {
	case f.Path == "":
		return f.Message
	case f.Line == 0:
		return fmt.Sprintf("%s: %s", f.Path, f.Message)
	default:
		return fmt.Sprintf("%s:%d: %s", f.Path, f.Line, f.Message)
	}
}
