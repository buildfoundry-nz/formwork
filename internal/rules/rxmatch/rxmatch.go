package rxmatch

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// Package rxmatch is the ONE regex backend every rule type compiles through.
//
// It was internal to package pattern until #17979: pair-consistency compiled
// with regexp.Compile directly, so `syntax: regexp2` and `multiline: true` were
// accepted by forbidden-pattern and refused by pair-consistency. A repo
// converting a rule from one to the other had to drop those params, and
// dropping multiline SILENTLY DISABLES a cross-line trigger — the rule still
// loads, still reports OK, and no longer matches anything. Extracting the
// matcher rather than copying it is the point: two regex backends are how two
// answers to one question start to disagree.
//
// Matcher matches text (spec §5 pattern semantics). Two backends: Go's RE2
// (default — fast, linear-time, no catastrophic backtracking) and the regexp2
// engine, opt-in via `syntax: regexp2`, for the minority of rules that need
// PCRE2 lookaround/backreferences the shell (pcre2) gates used. MatchString is
// the per-line path; FindLine (used by `multiline: true`) matches over whole
// file content and returns the 1-based line where the first match starts. Both
// return an error so a regexp2 backtracking timeout fails closed (exit 2)
// rather than reading as a clean pass — a rule that failed to evaluate must
// never look like one that found nothing (#22).
type Matcher interface {
	MatchString(s string) (bool, error)
	FindLine(content string) (int, bool, error)
	// FindIndex reports the byte offset where the first match starts, so a
	// caller can inspect the text immediately before it (params.denied_by,
	// #4). Returns -1 when there is no match; a matcher that cannot report a
	// position returns -1 with ok=true, which the denial stage treats as
	// undecidable and therefore not a denial.
	FindIndex(s string) (int, bool, error)
	// CountMatches reports how many non-overlapping matches s holds. The
	// pair-consistency `countable` obligation needs it: count(requires) must be
	// >= count(trigger) inside a unit, so a second trigger cannot free-ride on
	// one companion. Returns an error for the same reason MatchString does — a
	// regexp2 timeout must not read as "zero matches", which would clear the
	// obligation it was meant to enforce.
	CountMatches(s string) (int, error)
	String() string
}

type re2Matcher struct{ re *regexp.Regexp }

func (m re2Matcher) MatchString(s string) (bool, error) { return m.re.MatchString(s), nil }
func (m re2Matcher) FindIndex(s string) (int, bool, error) {
	loc := m.re.FindStringIndex(s)
	if loc == nil {
		return -1, false, nil
	}
	return loc[0], true, nil
}
func (m re2Matcher) CountMatches(s string) (int, error) {
	return len(m.re.FindAllStringIndex(s, -1)), nil
}
func (m re2Matcher) String() string { return m.re.String() }
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
func (m pcreMatcher) FindIndex(s string) (int, bool, error) {
	mm, err := m.re.FindStringMatch(s)
	if err != nil {
		return -1, false, fmt.Errorf("regexp2 %q: %w", m.src, err)
	}
	if mm == nil {
		return -1, false, nil
	}
	// regexp2 indexes in RUNES, not bytes. Converting keeps the offset usable
	// as a byte slice bound; getting this wrong would hand the denial stage a
	// prefix cut mid-character on any non-ASCII line.
	return len(string([]rune(s)[:mm.Index])), true, nil
}

// CountMatches walks the match chain rather than calling a count helper
// regexp2 does not provide. A timeout mid-walk is returned, not truncated to
// the matches found so far: a short count clears a countable obligation, which
// is the silent pass this interface exists to prevent.
func (m pcreMatcher) CountMatches(s string) (int, error) {
	mm, err := m.re.FindStringMatch(s)
	if err != nil {
		return 0, fmt.Errorf("regexp2 %q: %w", m.src, err)
	}
	n := 0
	for mm != nil {
		n++
		mm, err = m.re.FindNextMatch(mm)
		if err != nil {
			return 0, fmt.Errorf("regexp2 %q: %w", m.src, err)
		}
	}
	return n, nil
}

func (m pcreMatcher) String() string { return m.src }

// Compile builds a matcher for pattern under syntax: "" or "re2" -> RE2;
// "regexp2" -> the PCRE2-capable engine (lookaround enabled, 1s match timeout).
// Any other syntax value is an error (exit-2 config error).
func Compile(what, pattern, syntax string) (Matcher, error) {
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
