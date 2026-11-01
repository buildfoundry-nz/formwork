// Package finding defines the result type every rule evaluation produces.
package finding

import "sort"

// Severity classifies a finding. Only error-severity findings affect the
// process exit code (spec §6).
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
)

// Finding is one rule violation. Path is repo-relative and slash-separated;
// it is empty for scope-level findings. Line is 1-based and zero when the
// finding is not anchored to a line.
type Finding struct {
	RuleID   string
	Severity Severity
	Path     string
	Line     int
	Message  string
}

// Sort orders findings deterministically by (RuleID, Path, Line, Message).
func Sort(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Message < b.Message
	})
}
