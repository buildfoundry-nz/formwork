package main

import (
	"strings"
	"testing"
)

// The probe lines are the calibration table's own (calibrate.go), so a test
// and the shipped self-check can never disagree about the shape being judged.
const (
	goLine   = calibrationGoLine
	dartLine = calibrationDartLine
)

func mustCompile(t *testing.T, pattern string) lineMatcher {
	t.Helper()
	m, err := compileLine(pattern, "")
	if err != nil {
		t.Fatalf("compileLine(%q): %v", pattern, err)
	}
	return m
}

// TestNarrowableOnFlagsAPrefixPattern is the shipped defect. The pattern stops
// at the ranged expression, so the slice that guts the walk is appended past
// the end of its match and the arm never notices.
func TestNarrowableOnFlagsAPrefixPattern(t *testing.T) {
	got, yes, err := narrowableOn(mustCompile(t, `range spec\.Sections`), goLine, langGo)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Fatal("`range spec\\.Sections` is a prefix of `range spec.Sections[:1]` but was not flagged")
	}
	if !strings.Contains(got, "spec.Sections[:1]") {
		t.Errorf("the defeating line %q does not carry the narrowing", got)
	}
}

// TestNarrowableOnFlagsAWordBoundaryThatIsNotOne — `\b` after an identifier
// reads like a terminator and is not one: the boundary between `Sections` and
// `[` is still a word boundary, so the narrowed line matches too. A syntactic
// "does the pattern end in an anchor" check waves this straight through, which
// is why the verdict is demonstrated rather than spelled.
func TestNarrowableOnFlagsAWordBoundaryThatIsNotOne(t *testing.T) {
	_, yes, err := narrowableOn(mustCompile(t, `range spec\.Sections\b`), goLine, langGo)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Fatal("a trailing \\b is not a terminator for a slice suffix, but the arm was spared")
	}
}

// TestNarrowableOnSparesABoundedPattern is the other half, and the half that
// decides whether this check can be kept green. An arm that pins the whole
// range clause is displaced by the narrowing and must never be flagged.
func TestNarrowableOnSparesABoundedPattern(t *testing.T) {
	for _, pat := range []string{
		`range spec\.Sections \{`,
		`range spec\.Sections\s*\{`,
		`range spec\.Sections\s`,
	} {
		_, yes, err := narrowableOn(mustCompile(t, pat), goLine, langGo)
		if err != nil {
			t.Fatal(err)
		}
		if yes {
			t.Errorf("bounded pattern %q was flagged — the narrowing displaces its terminator", pat)
		}
	}
}

// TestNarrowableOnIgnoresAMatchOutsideTheTraversal is the false-positive guard
// that keeps this check off the ~680 ordinary required-patterns. A pattern that
// merely shares a line with a loop head has not staked anything on that loop.
func TestNarrowableOnIgnoresAMatchOutsideTheTraversal(t *testing.T) {
	line := "\tfor _, r := range rows { db.WithTenantOrg(ctx, org) }"
	_, yes, err := narrowableOn(mustCompile(t, `WithTenantOrg\(`), line, langGo)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Error("a pattern matching past the traversal was blamed for that traversal")
	}
}

// TestNarrowableOnReadsDartForIn — the Dart half. `.take(1)` is the suffix
// there, and `for (… in …)` the head.
func TestNarrowableOnReadsDartForIn(t *testing.T) {
	got, yes, err := narrowableOn(mustCompile(t, `jobTypesOnly`), dartLine, langDart)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Fatal("a bare Dart selector at a for-in source was not flagged")
	}
	if !strings.Contains(got, "jobTypesOnly.take(1)") {
		t.Errorf("the defeating line %q does not carry the narrowing", got)
	}
	_, yes, err = narrowableOn(mustCompile(t, `jobTypesOnly\)`), dartLine, langDart)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Error("a Dart pattern closing its own paren was flagged")
	}
}

// TestNarrowableOnIgnoresNonSourceLanguages — a TSV row or a YAML key can hold
// the word `range` without being a traversal anything can narrow.
func TestNarrowableOnIgnoresNonSourceLanguages(t *testing.T) {
	_, yes, err := narrowableOn(mustCompile(t, `range spec\.Sections`), goLine, langOther)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Error("a non-source line was judged as a traversal")
	}
}

// TestTraversalSpansBoundTheRangedExpression — the expression must be bounded
// by its OWN brackets, or `range f(a, b) {` is cut at the comma and the
// narrowing is spliced into the middle of an argument list.
func TestTraversalSpansBoundTheRangedExpression(t *testing.T) {
	line := "\tfor _, b := range SplitOnConflict(g.Members, country) {"
	spans := traversalSpans(line, langGo)
	if len(spans) != 1 {
		t.Fatalf("spans: %d, want 1", len(spans))
	}
	if got := line[spans[0].start:spans[0].end]; got != "SplitOnConflict(g.Members, country)" {
		t.Errorf("ranged expression %q", got)
	}
}

// TestLangOfClassifiesBySuffix — the narrowing is language-specific, so a
// misclassified file would splice Go slice syntax into Dart.
func TestLangOfClassifiesBySuffix(t *testing.T) {
	for path, want := range map[string]lang{
		"internal/x/y.go":         langGo,
		"lib/presentation/z.dart": langDart,
		"docs/a.md":               langOther,
		"scripts/b.sql":           langOther,
	} {
		if got := langOf(path); got != want {
			t.Errorf("langOf(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestNarrowableOnReadsAWrappedDartForIn — the ORDINARY spelling, not the tidy
// one. `dart format` does not keep a for-in header on one line: past the line
// limit it breaks BEFORE the `in`, leaving the traversal source alone on a
// continuation line, and the repo's dart-format gate makes that wrapping the
// only legal spelling of a long loop. Measured on
// packages/feature_templates/lib/presentation/template_editor_content.dart:
// lengthening the loop variable and the collection made dart format produce
//
//	for (final someRatherLongLoopVariableNameIndeed
//	    in jobTypes.jobTypesOnly.whereTheNameIsLong) {
//
// and the census reported 0 flagged while `jobTypesOnly` was still defeated by
// `.take(1)` on that second line. An arm that only sees the single-line form
// catches the shape a fixture author writes and misses the shape the formatter
// produces (#15721).
func TestNarrowableOnReadsAWrappedDartForIn(t *testing.T) {
	const wrapped = "        in jobTypes.jobTypesOnly.whereTheNameIsLong) {"
	got, yes, err := narrowableOn(mustCompile(t, `jobTypesOnly`), wrapped, langDart)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Fatal("a dart-format-wrapped for-in continuation line was not read as a traversal, so the arm was spared")
	}
	if !strings.Contains(got, "take(1)") {
		t.Errorf("the defeating line %q does not carry the narrowing", got)
	}
}

// TestWrappedDartHeadIgnoresOrdinaryProse — the false-positive side of reading
// a bare continuation line. `in` opens no traversal in a comment, a string or
// an argument list, and a check that thought otherwise would blame arms for
// loops that are not there.
func TestWrappedDartHeadIgnoresOrdinaryProse(t *testing.T) {
	for _, line := range []string{
		"    // in the same order the workflow declares them",
		"    final label = 'in progress';",
		"    return items.where((i) => i.isIn(scope));",
		"    print('cast in place');",
	} {
		if got := traversalSpans(line, langDart); len(got) != 0 {
			t.Errorf("%q yielded %d traversal span(s), want 0 — prose is not a loop head", line, len(got))
		}
	}
}
