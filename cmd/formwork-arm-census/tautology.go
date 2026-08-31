package main

import (
	"fmt"
	"regexp"
	"time"

	"github.com/dlclark/regexp2"
)

// probeLines is the arbitrary-line corpus a required pattern is tested
// against. Every entry is a line no rule in this repo is about: punctuation,
// whitespace, a digit, a non-ASCII rune, an empty-ish run. Nothing here is Go,
// Dart, SQL or YAML vocabulary, so a pattern that pins a real token cannot
// match them all by accident.
//
// The verdict is DELIBERATELY empirical rather than a list of forbidden
// spellings. `.` and `.*` are the two spellings that happen to be in the tree
// today; `[\s\S]*`, `(?s).*`, `[^]`, `.{0,}` and `[a-z]*` are the same rule with
// different characters, and a syntactic blocklist is an invitation to write the
// next one. A required pattern that matches an arbitrary line requires nothing
// of the file beyond having a line at all — which is what the check asks.
//
// The zero-width case is covered by construction: a pattern like `[a-z]*` or
// `x?` matches every probe through its empty match, and MatchString is
// unanchored, so it is flagged exactly like `.*` is.
var probeLines = []string{
	"}",
	"0",
	"   ",
	"\t",
	"-",
	"λ",
	"a b c",
	";;",
	" ",
	"()",
}

// tautological reports whether a required-pattern's regex matches every probe
// line — i.e. whether the pattern can distinguish a compliant file from any
// other non-empty one.
//
// Compilation mirrors the engine's own compileMatcher: RE2 by default,
// dlclark/regexp2 under `syntax: regexp2`, with the same one-second match
// timeout. An unknown syntax or an uncompilable pattern is an ERROR, never a
// quiet "not tautological" — the engine would have refused to load that rule,
// so a census that swallowed it would be reporting on a corpus the gate cannot
// run.
func tautological(pattern, syntax string) (bool, error) {
	switch syntax {
	case "", "re2":
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
		for _, p := range probeLines {
			if !re.MatchString(p) {
				return false, nil
			}
		}
		return true, nil
	case "regexp2":
		re, err := regexp2.Compile(pattern, regexp2.None)
		if err != nil {
			return false, fmt.Errorf("invalid regexp2 pattern %q: %w", pattern, err)
		}
		re.MatchTimeout = time.Second
		for _, p := range probeLines {
			ok, err := re.MatchString(p)
			if err != nil {
				return false, fmt.Errorf("regexp2 %q: %w", pattern, err)
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unknown syntax %q (want re2 or regexp2)", syntax)
	}
}
