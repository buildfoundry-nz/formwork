// Package fixturetest runs each rule against its fire/pass fixture trees
// and asserts findings match declared expectations exactly (spec §7).
package fixturetest

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// expectation is one anticipated finding. Path "" means scope-level;
// Line 0 means file-level (not line-anchored).
//
// MessagePin, when non-empty, is a required substring of finding.Message.
// Path+Line alone cannot pin offender identity that lives only in Message
// (set-relation missing elements, pattern-count counts, etc.). The field is
// named MessagePin — not Message — so its field-set does not collide with
// rules.Match (Path, Line, Message).
type expectation struct {
	Path       string
	Line       int
	MessagePin string
}

func (e expectation) String() string {
	base := ""
	switch {
	case e.Path == "":
		base = "- (scope-level)"
	case e.Line == 0:
		base = e.Path
	default:
		base = fmt.Sprintf("%s:%d", e.Path, e.Line)
	}
	if e.MessagePin == "" {
		return base
	}
	return base + " message~" + e.MessagePin
}

// collectExpectations gathers a fixture's expected findings from two
// sources: inline `want: <rule-id>` markers on violating lines, and an
// optional sibling manifest `<dir>.want` (kept outside the scan root so it
// can never trip the rule) for file-level and scope-level findings.
//
// .want line shapes (message pin is optional, space-separated after the
// location token):
//
//   - scope-level, no message pin (back-compat)
//   - <message substring>     scope-level with message pin
//     path                      file-level
//     path:line                 line-anchored
//     path:line <message>       line-anchored with message pin
func collectExpectations(fset *scan.FileSet, dir, ruleID string) ([]expectation, error) {
	marker := regexp.MustCompile(`want:\s*` + regexp.QuoteMeta(ruleID) + `(\s|$)`)
	var out []expectation
	for _, f := range fset.Files {
		lines, err := f.Lines()
		if err != nil {
			return nil, err
		}
		for i, line := range lines {
			if marker.MatchString(line) {
				out = append(out, expectation{Path: f.Path(), Line: i + 1})
			}
		}
	}

	data, err := os.ReadFile(dir + ".want")
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for n, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "-" {
			out = append(out, expectation{})
			continue
		}
		if strings.HasPrefix(line, "- ") {
			msg := strings.TrimSpace(line[2:])
			if msg == "" {
				out = append(out, expectation{})
				continue
			}
			out = append(out, expectation{MessagePin: msg})
			continue
		}
		// path:line or path:line message — line number must be the whole
		// token after the first colon so "a.go:3 free-ride" parses.
		if path, rest, ok := strings.Cut(line, ":"); ok {
			numTok, msg, _ := strings.Cut(rest, " ")
			ln, convErr := strconv.Atoi(numTok)
			if convErr != nil {
				return nil, fmt.Errorf("%s.want:%d: bad line number %q", dir, n+1, numTok)
			}
			out = append(out, expectation{Path: path, Line: ln, MessagePin: strings.TrimSpace(msg)})
			continue
		}
		out = append(out, expectation{Path: line})
	}
	return out, nil
}

// diff compares findings against expectations and returns sorted
// human-readable problems; empty means every expectation is satisfied and
// every finding is claimed.
//
// Matching rules:
//   - Path and Line must match exactly (empty Path = scope-level).
//   - When expectation.MessagePin is non-empty, finding.Message must contain it.
//   - When expectation.MessagePin is empty, Message is ignored (back-compat).
//
// Findings and expectations are multisets: each finding may satisfy at most
// one expectation.
func diff(findings []finding.Finding, expected []expectation) []string {
	used := make([]bool, len(findings))
	var problems []string
	for _, e := range expected {
		matched := false
		for i, f := range findings {
			if used[i] {
				continue
			}
			if f.Path != e.Path || f.Line != e.Line {
				continue
			}
			if e.MessagePin != "" && !strings.Contains(f.Message, e.MessagePin) {
				continue
			}
			used[i] = true
			matched = true
			break
		}
		if !matched {
			problems = append(problems, "missing expected finding "+e.String())
		}
	}
	for i, f := range findings {
		if used[i] {
			continue
		}
		// Unpinned unexpected: report Path+Line only so existing messages stay stable.
		problems = append(problems, "unexpected finding "+expectation{Path: f.Path, Line: f.Line}.String())
	}
	sort.Strings(problems)
	return problems
}
