package vcs

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// splitRange turns the operator's `--range` string into the argv git receives.
// It is the single tokenizer every reader of a range string uses — rangePaths,
// RangeModes and Diff — and it replaces `strings.Fields` (#99).
//
// WHY FIELDS WAS A FAIL-OPEN, NOT A LIMITATION. A range string may legally
// carry a `-- <pathspec>` tail (#97 made those work and thereby made them a
// supported caller shape), and a pathspec whose NAME contains a space cannot
// survive a whitespace split: `HEAD~1..HEAD -- my dir` reached git as the two
// pathspecs `my` and `dir`. Unmatched diff pathspecs are NOT errors — git exits
// 0 with empty output — so RangePaths returned an empty set, every per-file rule
// saw zero files, and the gate exited 0 over a changeset nobody scanned,
// indistinguishable from "no changes in range". Measured on git 2.50.1.
//
// THE GRAMMAR IS SHELL-LIKE, AND DELIBERATELY THE SMALL PART OF IT.
//
//   - Unquoted whitespace separates arguments, exactly as before. Every range
//     string carrying no quote and no backslash tokenizes byte-identically to
//     `strings.Fields`, which is what keeps #97's space-free multi-pathspec
//     tails working (TestRangePathsKeepsMultiplePathspecsSeparate).
//   - `'…'` is literal: no escape has any meaning inside it.
//   - `"…"` is literal except that a backslash escapes `"` and `\`.
//   - Outside quotes a backslash escapes the next character, whatever it is.
//
// There is no variable expansion, no globbing, no comment character and no
// operator vocabulary: this tokenizes ONE string into arguments, it does not run
// a shell. Anything git's own pathspec magic understands (`:(glob)`, `:!x`)
// passes through untouched, because it is text to this function.
//
// A BACKSLASH IS NOW AN ESCAPE, which is the one incompatible half. A tail
// spelled with Windows separators (`-- dir\sub`) previously reached git as
// `dir\sub` and now reaches it as `dirsub`. The trade is accepted rather than
// overlooked: this package's whole path vocabulary is slash-separated
// (filepath.ToSlash on every record git returns), git accepts forward slashes
// as pathspec separators on Windows, and dropping the escape would leave a name
// carrying a quote character with no spelling at all.
//
// AN UNFINISHED QUOTE OR ESCAPE IS REFUSED, NOT GUESSED. Closing it silently
// would put this function back in the business of deciding what the operator
// meant, and the decision it used to make wrong is the whole of #99. The error
// travels up as exit 2 through every caller — a range formwork cannot tokenize
// must never read as a range that matched nothing.
//
// THAT REFUSAL IS A REGRESSION FOR ONE SPELLING, and it is a deliberate one:
// `-- it's.ts` used to reach git whole and now refuses, because a lone
// apostrophe is an unfinished quote here. A filename with an apostrophe is not
// exotic, so the cost is real; it is taken because the alternative is a
// tokenizer that closes quotes by guessing, which is #99 again. The spellings
// that still work are `-- "it's.ts"` and `-- it\'s.ts`. Loud and one-line
// fixable beats silent and unscanned.
//
// WHAT THIS DOES NOT CLOSE, stated because the next reader will assume
// otherwise: a pathspec that matches nothing is still exit 0 with an empty set,
// and that is git's honest answer to a legitimate question — scoping a range to
// a subdirectory that did not change is an ordinary, correct pass. So an
// operator who writes `-- my dir` UNQUOTED still gets two pathspecs and an empty
// answer; what changed is that the spelling for what they meant now exists and
// works, not that the mistake became loud. Making "matched nothing" loud would
// need a universe to test the pathspec against, and neither the index nor HEAD
// is that universe for an arbitrary range.
func splitRange(rng string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		started bool // distinguishes an empty quoted field from no field at all
	)
	flush := func() {
		if started {
			args = append(args, cur.String())
			cur.Reset()
			started = false
		}
	}
	runes := []rune(rng)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case unicode.IsSpace(c):
			flush()
		case c == '\\':
			if i+1 >= len(runes) {
				return nil, fmt.Errorf("git: the range %q ends in a backslash with nothing to escape — double it (\\\\) for a literal backslash, or drop it", rng)
			}
			i++
			started = true
			cur.WriteRune(runes[i])
		case c == '\'' || c == '"':
			consumed, err := quoted(runes[i:], rng)
			if err != nil {
				return nil, err
			}
			started = true
			cur.WriteString(consumed.text)
			i += consumed.width - 1
		default:
			started = true
			cur.WriteRune(c)
		}
	}
	flush()
	if len(args) == 0 {
		return nil, errors.New("git: empty range")
	}
	return args, nil
}

// quotedRun is one quoted section: the text it contributes, and how many runes
// of the input it spanned INCLUDING both quote characters.
type quotedRun struct {
	text  string
	width int
}

// quoted reads one quoted section starting at the opening quote in runes[0].
// Single quotes are wholly literal; double quotes honour `\"` and `\\` only, so
// a backslash before anything else stays a backslash — which is what a shell
// does, and what keeps a Windows-ish `"a\b"` from silently losing a character.
func quoted(runes []rune, rng string) (quotedRun, error) {
	q := runes[0]
	var b strings.Builder
	for i := 1; i < len(runes); i++ {
		c := runes[i]
		if c == q {
			return quotedRun{text: b.String(), width: i + 1}, nil
		}
		if q == '"' && c == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
			i++
			b.WriteRune(runes[i])
			continue
		}
		b.WriteRune(c)
	}
	return quotedRun{}, fmt.Errorf("git: the range %q opens a %c quote that is never closed — formwork will not guess where the pathspec ends", rng, q)
}
