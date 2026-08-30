// Package marker implements the single shared grammar for the
// `formwork:allow <rule-id> <reason>` exemption marker (spec §5). The engine
// (suppression) and `formwork lint` (reasonless-marker hygiene) both consume
// Classify so the two consumers can never disagree about what counts as a
// valid marker.
package marker

import (
	"regexp"
	"strings"
	"unicode"
)

// Kind classifies a formwork:allow marker occurrence for one rule id on one
// line.
type Kind int

const (
	// None: no formwork:allow marker naming this rule id is present on the
	// line at all.
	None Kind = iota
	// Reasonless: a marker naming this rule id is present, but nothing that
	// counts as a real reason follows it.
	Reasonless
	// Reasoned: a marker naming this rule id is present with a valid
	// reason — the exemption is honored.
	Reasoned
)

// markerRE finds "formwork:allow", the whitespace-delimited id token that
// follows it, and (optionally) the rest of the line after further
// whitespace as the reason candidate. Using a whitespace-delimited token for
// the id (rather than a boundary assertion) is what makes prefix collisions
// impossible in both directions: a rule id of "no" never matches marker text
// "no-hit" and a rule id of "no-hit" never matches marker text "no", because
// the captured token must equal the rule id exactly.
var markerRE = regexp.MustCompile(`formwork:allow[ \t]+(\S+)(?:[ \t]+(.*))?`)

// closers are trailing comment-closer tokens that terminate common comment
// styles the marker gets written inside of. They never count toward a
// reason: "/* formwork:allow id */" and "<!-- formwork:allow id -->" carry
// no more of a real reason than a bare marker does.
var closers = []string{"*/", "-->", "#>"}

// Classify scans line for a formwork:allow marker naming ruleID. If more
// than one such marker occurs on the line, Reasoned wins if any occurrence
// is reasoned.
func Classify(line, ruleID string) Kind {
	best := None
	for _, m := range markerRE.FindAllStringSubmatch(line, -1) {
		if m[1] != ruleID {
			continue
		}
		if hasReason(m[2]) {
			return Reasoned
		}
		best = Reasonless
	}
	return best
}

// hasReason decides whether a reason candidate (the text after the marker's
// rule id) is a real reason: trim trailing whitespace and '\r', strip a
// trailing comment-closer token (repeating, since trimming can expose
// another), and require at least one alphanumeric character to remain.
func hasReason(candidate string) bool {
	s := strings.TrimRight(candidate, " \t\r")
	for {
		trimmed := false
		for _, c := range closers {
			if strings.HasSuffix(s, c) {
				s = strings.TrimSuffix(s, c)
				trimmed = true
			}
		}
		if !trimmed {
			break
		}
		s = strings.TrimRight(s, " \t\r")
	}
	return containsAlnum(s)
}

func containsAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
