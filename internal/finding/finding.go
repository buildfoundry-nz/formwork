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

	// Suppressed marks a finding exempted by an inline formwork:allow marker
	// or an allowlist entry (spec §5). Suppressed findings never affect the
	// exit code; lint consumes them for staleness detection.
	Suppressed   bool
	SuppressedBy string // "marker" or "allowlist:<file>:<line>"
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

// Unsuppressed returns the findings not marked Suppressed, preserving order.
func Unsuppressed(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if !f.Suppressed {
			out = append(out, f)
		}
	}
	return out
}
