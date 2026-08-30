package pattern

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// lineMatcher matches text (spec §5 pattern semantics). Two backends: Go's RE2
// (default — fast, linear-time, no catastrophic backtracking) and the regexp2
// engine, opt-in via `syntax: regexp2`, for the minority of rules that need
// PCRE2 lookaround/backreferences the shell (pcre2) gates used. MatchString is
// the per-line path; FindLine (used by `multiline: true`) matches over whole
// file content and returns the 1-based line where the first match starts. Both
// return an error so a regexp2 backtracking timeout fails closed (exit 2)
// rather than reading as a clean pass — a rule that failed to evaluate must
// never look like one that found nothing (#22).
type lineMatcher interface {
	MatchString(s string) (bool, error)
	FindLine(content string) (int, bool, error)
	String() string
}

type re2Matcher struct{ re *regexp.Regexp }

func (m re2Matcher) MatchString(s string) (bool, error) { return m.re.MatchString(s), nil }
func (m re2Matcher) String() string                     { return m.re.String() }
func (m re2Matcher) FindLine(content string) (int, bool, error) {
	loc := m.re.FindStringIndex(content)
	if loc == nil {
		return 0, false, nil
	}
	return 1 + strings.Count(content[:loc[0]], "\n"), true, nil
}

type pcreMatcher struct {
	re  *regexp2.Regexp
	src string
}

func (m pcreMatcher) FindLine(content string) (int, bool, error) {
	mm, err := m.re.FindStringMatch(content)
	if err != nil {
		return 0, false, fmt.Errorf("regexp2 %q: %w", m.src, err)
	}
	if mm == nil {
		return 0, false, nil
	}
	runes := []rune(content)
	idx := mm.Index
	if idx > len(runes) {
		idx = len(runes)
	}
	return 1 + strings.Count(string(runes[:idx]), "\n"), true, nil
}

// MatchString surfaces a regexp2 backtracking timeout as an error instead of
// swallowing it as no-match: the 1s match timeout still bounds a stuck
// guardrail, but the miss it would otherwise report reads as a pass, and the
// exit-code contract forbids a rule that failed to evaluate from passing (#22).
//
// Blast radius (deliberate): like any rule error, this propagates out of
// CheckFile and the engine keeps only the first error and aborts the whole run
// (exit 2) — every other rule's verdict is discarded, not just this rule's.
// That is the same fail-loud treatment a panicking or unreadable-file rule gets;
// a broken guardrail must down the run, not silently pass. The wrapped error
// names the offending rule and file (engine.checkFile), so the culprit is
// diagnosable. A single pathological committed file can therefore fail the
// suite until that file or the timing-out regexp2 rule is fixed — the intended,
// recoverable outcome over a silent under-report.
func (m pcreMatcher) MatchString(s string) (bool, error) {
	ok, err := m.re.MatchString(s)
	if err != nil {
		return false, fmt.Errorf("regexp2 %q: %w", m.src, err)
	}
	return ok, nil
}
func (m pcreMatcher) String() string { return m.src }

// compileMatcher builds a matcher for pattern under syntax: "" or "re2" -> RE2;
// "regexp2" -> the PCRE2-capable engine (lookaround enabled, 1s match timeout).
// Any other syntax value is an error (exit-2 config error).
func compileMatcher(what, pattern, syntax string) (lineMatcher, error) {
	switch syntax {
	case "", "re2":
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid pattern: %w", what, err)
		}
		return re2Matcher{re: re}, nil
	case "regexp2":
		re, err := regexp2.Compile(pattern, regexp2.None)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid regexp2 pattern: %w", what, err)
		}
		re.MatchTimeout = time.Second
		return pcreMatcher{re: re, src: pattern}, nil
	default:
		return nil, fmt.Errorf("%s: unknown syntax %q (want re2 or regexp2)", what, syntax)
	}
}
