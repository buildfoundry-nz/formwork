package pattern

import (
	"regexp/syntax"
	"strings"
)

// This file answers one question, statically: can this rule match anything that
// does NOT contain its prefilter literal? If the answer is no, the prefilter is
// provably a pure optimization and lint needs no data to say so — which is the
// whole point, because a tombstone rule offers no data (#133): nothing on the
// tree matches it, so a findings differential compares empty against empty.
//
// The 2026-07-27 prefilter-lint design rejected a *lexical* version of this
// test — "is the literal a substring of the pattern text" — as noisy and wrong
// on escaping (`platform\.material_cat` does not contain `platform.material_cat`
// as a substring). Everything here works on the PARSED pattern instead, so an
// escape is resolved to the character it denotes before anything is compared.
//
// The other half of earning the right to report is silence under doubt. Every
// construct this analysis cannot model exactly returns decidable=false, which
// is never a finding on its own. A false "load-bearing" would redden a correct
// rule, and a guardrail that cries wolf gets its prefilter deleted rather than
// examined.

// PrefilterImplied implements rules.PrefilterImplication.
//
// The rule fires only when EVERY conjunct matches, so the literal being implied
// by any one of them implies it for the rule as a whole. The conjuncts are:
//
//   - all_of mode: each all_of pattern.
//   - plain mode: the pattern AND every require_present guard. Missing the
//     guards was a real false positive, measured on a ported corpus: a rule
//     triggering on `\.broadcast\(` under a require_present of
//     `(?:implements|extends|with)\s+[A-Za-z]*ActivitySource` carries the
//     prefilter `ActivitySource`, which the trigger alone cannot imply but the
//     guard necessarily does. The guard is exactly where a rule author puts
//     the file-level fact a prefilter usually gates on, so ignoring it would
//     misreport the most idiomatic shape there is.
//
// require_absent is deliberately not a conjunct: it asserts what must NOT be
// present, so it can never guarantee the literal appears.
//
// The converse needs care: to name a counterexample we must know that NO
// conjunct implies the literal, which means every one has to be decidable first.
func (c *forbidden) PrefilterImplied() (implied, decidable bool, counterexample string) {
	if c.prefilter == "" {
		return false, false, ""
	}
	// regexp2 patterns use PCRE2 constructs regexp/syntax does not accept
	// (lookaround, backreferences). Parsing would fail or, worse, succeed with
	// a different meaning — so this analysis declines them outright.
	if c.syntax == "regexp2" {
		return false, false, ""
	}
	var conjuncts []string
	if len(c.allOfSrc) > 0 {
		conjuncts = c.allOfSrc
	} else {
		conjuncts = append([]string{c.src}, c.requirePresentSrc...)
	}
	for _, src := range conjuncts {
		if ok, dec, _ := sourceImplies(src, c.prefilter); dec && ok {
			return true, true, ""
		}
	}
	// Nothing proved it. A counterexample is only honest if every conjunct was
	// analysable — one we could not read might have been the one requiring it.
	var firstCE string
	for _, src := range conjuncts {
		_, dec, ce := sourceImplies(src, c.prefilter)
		if !dec {
			return false, false, ""
		}
		if firstCE == "" {
			firstCE = ce
		}
	}
	return false, true, firstCE
}

// sourceImplies parses one pattern source and reports whether every string it
// can match must contain lit.
func sourceImplies(src, lit string) (implied, decidable bool, counterexample string) {
	re, err := syntax.Parse(src, syntax.Perl)
	if err != nil {
		// A pattern that compiled for the matcher but will not parse here is a
		// gap in this analysis, not a defect in the rule: stay silent.
		return false, false, ""
	}
	return impliedBy(re, lit)
}

// impliedBy walks the alternation structure: the rule can match without lit
// exactly when SOME branch can, so every branch must carry it. A branch that
// cannot is returned as the counterexample, in its own regex source form —
// naming the branch is what makes the diagnostic actionable, and is the bar
// this analysis must clear before it is allowed to report at all.
func impliedBy(re *syntax.Regexp, lit string) (implied, decidable bool, counterexample string) {
	switch re.Op {
	case syntax.OpAlternate:
		for _, sub := range re.Sub {
			ok, dec, ce := impliedBy(sub, lit)
			if !dec {
				return false, false, ""
			}
			if !ok {
				return false, true, ce
			}
		}
		return true, true, ""
	case syntax.OpCapture:
		return impliedBy(re.Sub[0], lit)
	}
	runs, exact := guaranteedRuns(re)
	for _, r := range runs {
		if strings.Contains(r, lit) {
			// Sound even when exact is false: a literal run that MUST appear and
			// contains the prefilter settles it, whatever else went unmodelled.
			return true, true, ""
		}
	}
	if !exact {
		return false, false, ""
	}
	return false, true, re.String()
}

// guaranteedRuns returns maximal literal runs that every match of re must
// contain, and whether that set is exact. exact=false means "there may be
// required content this walk did not model" — callers may use the runs to
// PROVE implication, but must not conclude its absence.
//
// Adjacent literals in a concatenation are joined before being returned: the
// parser does not always merge them, and a prefilter routinely straddles the
// seam (`Alp` + `ha` must still satisfy a prefilter of `Alpha`).
func guaranteedRuns(re *syntax.Regexp) (runs []string, exact bool) {
	switch re.Op {
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			// A case-folded literal matches spellings the parser has already
			// normalized away, but the prefilter gate is a case-SENSITIVE
			// strings.Contains. Anything concluded from the folded form would
			// be about a different comparison than the one the engine runs.
			return nil, false
		}
		return []string{string(re.Rune)}, true

	case syntax.OpCapture:
		return guaranteedRuns(re.Sub[0])

	case syntax.OpPlus:
		return guaranteedRuns(re.Sub[0]) // one repetition is guaranteed

	case syntax.OpRepeat:
		if re.Min >= 1 {
			return guaranteedRuns(re.Sub[0])
		}
		return nil, true // may match empty — contributes nothing, and that is exact

	case syntax.OpStar, syntax.OpQuest:
		return nil, true // likewise optional, and known to be optional

	case syntax.OpConcat:
		var out []string
		var cur strings.Builder
		allExact := true
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpLiteral && sub.Flags&syntax.FoldCase == 0 {
				cur.WriteString(string(sub.Rune))
				continue
			}
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			subRuns, subExact := guaranteedRuns(sub)
			out = append(out, subRuns...)
			if !subExact {
				allExact = false
			}
		}
		if cur.Len() > 0 {
			out = append(out, cur.String())
		}
		return out, allExact

	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary,
		syntax.OpNoWordBoundary:
		return nil, true // zero-width: contributes no literal, and that is exact

	default:
		// OpAlternate nested inside a concatenation, character classes,
		// OpAnyChar, and anything added to the syntax package later. A nested
		// alternation could still guarantee the literal in every branch, but
		// modelling that means intersecting run sets; declining is the
		// conservative choice and costs only silence.
		return nil, false
	}
}
