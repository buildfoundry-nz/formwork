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
// order but are conventionally pre-sorted by the engine.
//
// The scan block sits between the per-rule verdicts and the summary line: the
// verdicts are what the run found, the block is what it looked at to find it,
// and the summary line stays last because habits and downstream tooling read it
// there.
func Human(w io.Writer, rls []*config.Rule, findings []finding.Finding, scan ScanSummary) {
	byRule := map[string][]finding.Finding{}
	for _, f := range findings {
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
	fmt.Fprintf(w, "formwork: %d/%d rules passed, %d finding(s)\n", passed, len(rls), len(findings))
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
