// embedded.go — the fourth way a rule ARM can be written so that it cannot
// fail: a `required-pattern` whose pattern is a SUBSTRING of a longer
// identifier that already appears in its own scope.
//
// #11934. `defaulttemplates-formula-validate-item-branch` pinned the literal
// `it\.Formula` over formula_validate.go. Two branches below, the kit walk
// reads `kit.Formula` — and `it.Formula` is a substring of `kit.Formula`.
// Deleting the ENTIRE root-item branch, which is the #8812 regression the arm
// is named for, left it green off the kit branch's own text.
//
// The verdict is DEMONSTRATED, not inferred from the pattern's spelling. For
// each arm the census reads the lines its own scope and preprocessing produce,
// removes every occurrence that stands as its OWN token, and asks whether the
// file still satisfies the arm. If it does, what is holding the arm up is text
// belonging to some other construct, and deleting the thing the rule is about
// changes nothing.
//
// Two conditions, and the second is what keeps honest arms out. The arm must
// have at least one token-aligned witness — that is the construct it is
// actually about — AND a match must survive its deletion. A pattern with no
// aligned witness at all is a deliberate substring spelling (`Repo` meaning
// `UserRepo`, `OrderRepo`), and deleting those identifiers DOES fail it, so it
// is not in this class and is never flagged.
package main

import (
	"fmt"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// isIdentByte reports whether b can continue an identifier in Go or Dart, and
// so whether a match that abuts it is really part of a longer token. `.` is
// deliberately NOT one: `it.Formula` inside `kit.Formula` is the shipped
// defect, and treating the selector dot as a token character would call that
// match aligned and spare the arm.
func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// tokenAligned reports whether the match [s,e) in text stands as its own token
// run — nothing that could continue an identifier abuts either end.
//
// BOTH ends are read: `it.Formula` inside `kit.Formula` abuts only on the
// left, so a right-end-only check waves the shipped spelling through.
//
// And BOTH sides of each end are read — the match's own edge character as well
// as the neighbour. A token only continues where two identifier characters
// meet. Testing the neighbour alone calls `ref\.onDispose\(` embedded in
// `ref.onDispose(authListenable...)`, because a letter follows the match; but
// the match ends at `(`, which cannot continue anything. Measured on this
// corpus, that one-sided test flagged 5 honest arms out of 7 — the
// over-firing rate that gets a rule disabled inside a week.
func tokenAligned(text string, s, e int) bool {
	if s > 0 && isIdentByte(text[s]) && isIdentByte(text[s-1]) {
		return false
	}
	if e < len(text) && isIdentByte(text[e-1]) && isIdentByte(text[e]) {
		return false
	}
	return true
}

// stripAlignedMatches removes every token-aligned occurrence of m from line,
// repeating until none remain, and reports the TEXT each removal matched. What
// is left is what would still be there after the construct the rule names is
// deleted, and the texts are what the rule legitimately names.
//
// The loop re-scans after each removal rather than collecting all spans up
// front, because splicing shifts every later offset and can expose a match the
// first scan could not see. It terminates because the line strictly shrinks on
// every iteration.
func stripAlignedMatches(m lineMatcher, line string) (string, []string, error) {
	var got []string
	for {
		loc, err := m.find(line)
		if err != nil {
			return "", nil, err
		}
		if loc == nil {
			return line, got, nil
		}
		if !tokenAligned(line, loc[0], loc[1]) {
			// The leftmost match is embedded. Anything aligned further right
			// is still reachable, so walk past this one rather than stopping:
			// returning here would call the whole line embedded on the
			// strength of its first match alone.
			rest, more, err := stripAlignedMatches(m, line[loc[1]:])
			if err != nil {
				return "", nil, err
			}
			return line[:loc[1]] + rest, append(got, more...), nil
		}
		got = append(got, line[loc[0]:loc[1]])
		line = line[:loc[0]] + line[loc[1]:]
	}
}

// satisfiedByEmbeddedOnly reports whether the arm still matches these lines
// once every token-aligned occurrence is gone, and returns the surviving line
// with its stripped form.
//
// Two gates gate the verdict, and both are false-positive safety.
//
// FIRST, the arm must have a token-aligned witness somewhere. A pattern with
// none is a deliberate substring spelling — `Repo` meaning `UserRepo`,
// `OrderRepo` — and deleting those identifiers DOES fail it, so it is not in
// this class.
//
// SECOND, the surviving match must be the SAME TEXT as one of those aligned
// witnesses. That is what makes this the substring class rather than a
// complaint about breadth: the defect is one literal doing double duty, named
// legitimately in one place and occurring by accident inside a longer token in
// another. Where an ALTERNATION has one alternative aligned and a DIFFERENT
// one surviving, those are two obligations, not a coincidence, and there is
// nothing an author could bind. Measured on this corpus that gate is the
// difference between 2 flagged and 1: `dart-tests-import-subject` lists six
// import prefixes, is held up by `package:tqs_` while `package:takeoffqs_schema`
// stands aligned, and has no cure in this class.
func satisfiedByEmbeddedOnly(m lineMatcher, lines []string) (orig, stripped string, yes bool, err error) {
	aligned := map[string]bool{}
	type survivor struct {
		at            int
		clean, wasHit string
	}
	var found *survivor
	for i, ln := range lines {
		cleaned, hits, err := stripAlignedMatches(m, ln)
		if err != nil {
			return "", "", false, err
		}
		for _, h := range hits {
			aligned[h] = true
		}
		if found != nil {
			continue
		}
		loc, err := m.find(cleaned)
		if err != nil {
			return "", "", false, err
		}
		if loc != nil {
			found = &survivor{at: i, clean: cleaned, wasHit: cleaned[loc[0]:loc[1]]}
		}
	}
	if found == nil || !aligned[found.wasHit] {
		return "", "", false, nil
	}
	return lines[found.at], found.clean, true, nil
}

// detectEmbedded flags every required-pattern arm that a longer identifier in
// its own scope keeps green.
func detectEmbedded(root string, arms []arm) ([]offender, int, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*config.Rule, len(cfg.Rules))
	for _, r := range cfg.Rules {
		byID[r.ID] = r
	}
	fset, err := scan.Walk(root)
	if err != nil {
		return nil, 0, err
	}
	var bad []offender
	examined := 0
	for _, a := range arms {
		if a.Type != "required-pattern" || a.Pattern == "" {
			continue
		}
		r, ok := byID[a.ID]
		if !ok {
			// The corpus reader saw an arm the engine did not load. A
			// loader disagreement is an error, never a quiet skip.
			return nil, 0, fmt.Errorf("%s:%d: arm %q is declared but the engine did not load it", a.File, a.Line, a.ID)
		}
		examined++
		m, err := compileLine(a.Pattern, a.Syntax)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d (%s): %w", a.File, a.Line, a.ID, err)
		}
		o, err := embeddedWitness(a, r, m, fset.Files)
		if err != nil {
			return nil, 0, err
		}
		if o != nil {
			bad = append(bad, *o)
		}
	}
	return bad, examined, nil
}

// embeddedWitness returns the first file in r's scope the arm survives in on
// embedded text alone, read through r's own preprocessing so the census sees
// exactly the text the gate sees. One witness is enough: the cure is to bind
// the pattern to the construct it is about, which removes every witness at
// once.
func embeddedWitness(a arm, r *config.Rule, m lineMatcher, files []*scan.File) (*offender, error) {
	for _, f := range files {
		if !r.Applies(f.Path()) {
			continue
		}
		v, err := f.Variant(r.Preprocess)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
		}
		lines, err := v.Lines()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
		}
		orig, stripped, yes, err := satisfiedByEmbeddedOnly(m, lines)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
		}
		if !yes {
			continue
		}
		return &offender{a.File, a.Line, a.ID, fmt.Sprintf(
			"pattern %q is a substring of a longer token in its own scope, so deleting every place it stands "+
				"as its own token leaves the arm green. In %s the surviving line reads\n       %s\n"+
				"     and with the arm's own witnesses removed it still matches\n       %s\n"+
				"     Bind the pattern to the construct it is about — its call site, its argument, its "+
				"surrounding punctuation — so no longer identifier can stand in for it.",
			a.Pattern, f.Path(), strings.TrimSpace(orig), strings.TrimSpace(stripped))}, nil
	}
	return nil, nil
}
