package main

import (
	"fmt"
	"io"
)

// Instrument calibration — the census proves its own verdict machinery before
// it reports on the corpus, the same way the vacuity census self-tests its
// matcher and per-glob expansion before printing a population.
//
// Two reasons it is not optional.
//
// FIRST, it is the only thing standing between a broken detector and a green
// gate. Every failure mode of these two checks is SILENT: invert the probe test
// and honest patterns get flagged (loud, and someone fixes it) — but widen
// isLastWitnessShape by one and the check starts flagging stated design
// thresholds, which is the one way it could do net harm; narrow it and it stops
// seeing the population it exists for, reporting OK forever. A census that
// reports "0 flagged" because it can no longer recognise an offender is exactly
// the vacuity this tool was built to find, one level up.
//
// SECOND, it is what makes the rules PROVABLE. mutation-proof materialises each
// rule into a scratch corpus PRUNED TO THAT RULE ALONE (prove.go), so the
// .formwork/rules/ these checks read contains one `type: command` arm and
// nothing else — no required-pattern to judge, no existence obligation to
// count. In that corpus no edit to the verdict logic can change the verdict,
// because there is nothing left to have a verdict about. The calibration runs
// first and is corpus-independent, so a mutation that breaks the instrument
// fails the gate wherever it runs.

// calibrationCase is one (input, expected verdict) pair for the tautology
// probe. Chosen so that both directions of failure are covered: a spelling of
// "match anything" that must be caught (including the two ZERO-WIDTH ones a
// blocklist waves through), and a real token that must not be.
type calibrationCase struct {
	pattern string
	syntax  string
	want    bool
	why     string
}

var tautologyCalibration = []calibrationCase{
	{".", "", true, "`.` matches any character — the spelling that shipped"},
	{".*", "", true, "the same rule, one character longer"},
	{`[\s\S]*`, "", true, "the same rule with a character class"},
	{"[a-z]*", "", true, "ZERO-WIDTH: matches every line through its empty match"},
	{"x?", "", true, "ZERO-WIDTH: same, and named in no blocklist"},
	{`(?s).*`, "regexp2", true, "the regexp2 backend must reach the same verdict"},
	{`WithTenantOrg\(`, "", false, "a real token must never be flagged"},
	{`^CREATE TABLE`, "", false, "an anchored real token must never be flagged"},
	{`^$`, "", false, "matches only an empty line — restrictive, not tautological"},
	{`claddinggrouptotals\.(List|Queue)\((?![^()]*,\s*nil\))`, "regexp2", false,
		"a regexp2 lookaround pattern must never be flagged"},
}

// witnessCalibration pins isLastWitnessShape on the four shapes the check has
// to tell apart. The `at-least 2` row is the load-bearing one: an explicit
// n >= 2 is a STATED cardinality (tqs-package-consumers-tqs-core is at-least 2
// over 42 witnesses because the invariant IS ">= 2 consumers", #6858), and
// flagging it would fail the build on every legitimate dependency drop — a rule
// with that false-positive rate gets disabled, which is worse than the vacuity
// it replaced.
var witnessCalibration = []struct {
	a    arm
	want bool
	why  string
}{
	{arm{Type: "required-pattern", Mode: "exists"}, true, "`mode: exists` is the accidental floor of one"},
	{arm{Type: "pattern-count", Op: "at-least", N: 1}, true, "`at-least 1` is the same floor, spelled"},
	{arm{Type: "pattern-count", Op: "at-least", N: 2}, false, "an explicit n >= 2 is a STATED cardinality (#6858)"},
	{arm{Type: "pattern-count", Op: "at-least", N: 517}, false, "a cured ratchet must not be flagged again"},
	{arm{Type: "required-pattern", Mode: "every-file"}, false, "every-file is not an existence obligation"},
	{arm{Type: "forbidden-pattern"}, false, "deleting the subject SATISFIES a forbidden arm"},
	{arm{Type: "pattern-count", Op: "at-most", N: 1}, false, "a ceiling is not a floor"},
}

// narrowableCalibration pins the narrowable verdict on the shapes it has to
// tell apart. Two rows carry the whole correctness of the check.
//
// The `\b` row is why the verdict is DEMONSTRATED rather than spelled: a
// trailing word boundary reads like a terminator and is not one, because the
// boundary between `Sections` and `[` still holds. Any syntactic "does the
// pattern end in an anchor" rule waves the shipped defect through in its
// second spelling.
//
// The WithTenantOrg row is the false-positive side, and the one that decides
// whether this check can be kept green: ~680 required-pattern arms in this
// corpus pin an ordinary token, and some of them share a line with a loop head
// they say nothing about. Blaming an arm for a traversal its match ran past
// would flag dozens of honest rules, and a rule nobody can keep green gets
// disabled — worse than the hole it replaced.
var narrowableCalibration = []struct {
	pattern string
	line    string
	lang    lang
	want    bool
	why     string
}{
	{`range spec\.Sections`, calibrationGoLine, langGo, true,
		"the shipped defect: the pattern is a prefix of `range spec.Sections[:1]`"},
	{`range spec\.Sections\b`, calibrationGoLine, langGo, true,
		"a trailing \\b is NOT a terminator for a slice suffix — the spelling a syntactic check misses"},
	{`range spec\.Sections\s*\{`, calibrationGoLine, langGo, false,
		"a bounded arm — the narrowing displaces its `{`"},
	{`range spec\.Sections `, calibrationGoLine, langGo, false,
		"a trailing literal space is a real terminator"},
	{`WithTenantOrg\(`, calibrationSharedLine, langGo, false,
		"a match that ran PAST the traversal has staked nothing on it"},
	{`jobTypesOnly`, calibrationDartLine, langDart, true,
		"the Dart half — `.take(1)` narrows a for-in source the same way"},
	{`jobTypesOnly\)`, calibrationDartLine, langDart, false,
		"closing its own paren is a real terminator"},
	{`jobTypesOnly`, calibrationDartWrappedLine, langDart, true,
		"the spelling dart format EMITS — it breaks before the `in`, and reading only the one-line header misses it (#15721)"},
	{`range spec\.Sections`, calibrationGoLine, langOther, false,
		"a line in a language with no suffix narrowing is never a traversal"},
}

// The three probe lines. Real shapes: the #11934 spec walk, an ordinary token
// sharing a line with a loop head, and a Dart job-type surface.
const (
	calibrationGoLine     = "\tfor _, sec := range spec.Sections {"
	calibrationSharedLine = "\tfor _, r := range rows { db.WithTenantOrg(ctx, org) }"
	calibrationDartLine   = "    for (final j in jobTypes.jobTypesOnly) {"
	// The same loop after dart format wrapped it. Reproduced by lengthening
	// the loop variable and the collection in template_editor_content.dart
	// until the header passed the line limit; the formatter broke before the
	// `in` and the census then reported 0 flagged.
	calibrationDartWrappedLine = "        in jobTypes.jobTypesOnly.whereTheNameIsLong) {"
)

// embeddedCalibration pins the embedded-substring verdict on the shapes it has
// to tell apart. Two rows carry the whole correctness of the check.
//
// The `Repo` row is the false-positive side and the one that decides whether
// this check can be kept green: a pattern with NO token-aligned witness is a
// deliberate substring spelling, and deleting those identifiers DOES fail it.
// Flagging that class would put honest arms on the report, and an arm that
// over-fires is disabled inside a week.
//
// The `it\.Formula` row is the shipped defect: the match abuts `k` on the LEFT
// only, so any alignment test reading the right end alone spares it.
var embeddedCalibration = []struct {
	pattern string
	lines   []string
	want    bool
	why     string
}{
	{`it\.Formula`, calibrationBranchLines, true,
		"the shipped defect: deleting the root-item branch leaves the arm green off `kit.Formula`"},
	{`"item", it\.Formula`, calibrationBranchLines, false,
		"the cure — bound to its own call site, no longer identifier can stand in for it"},
	{`Repo`, []string{"\tusers := NewUserRepo(db)"}, false,
		"NO aligned witness: a deliberate substring spelling, and deleting the identifier does fail it"},
	{`ValidateSpecFormulas`, []string{"\tif err := ValidateSpecFormulas(spec); err != nil {"}, false,
		"every witness stands as its own token, so deleting them all fails the arm"},
	{`ref\.onDispose\(`, []string{"    ref.onDispose(authListenable.dispose);"}, false,
		"a match ending at `(` is aligned however its neighbour reads — the one-sided test flagged 5 honest arms out of 7"},
	{`(package:takeoffqs_schema|package:tqs_)`, calibrationImportLines, false,
		"an ALTERNATION with one alternative aligned and a DIFFERENT one surviving is two obligations, not one literal doing double duty — and has no cure in this class"},
}

// Two imports from a real test file. The `package:takeoffqs_schema`
// alternative stands aligned and the `package:tqs_` alternative survives
// inside `package:tqs_core` — different text, so nothing an author could bind.
var calibrationImportLines = []string{
	"import 'package:tqs_core/testing/stub_dio.dart';",
	"import 'package:takeoffqs_schema/takeoffqs/api/v1/account_service.pb.dart';",
}

// The two branch lines the item-formula arm was measured on, from
// formula_validate.go: the root-item call site, and the kit call site two
// levels down whose `kit.Formula` contains it.
var calibrationBranchLines = []string{
	"\tif err := validateOneFormula(jobType, sectionKey, it.Description, \"item\", it.Formula); err != nil {",
	"\t\tif err := validateOneFormula(jobType, sectionKey, kitLoc, \"kit\", kit.Formula); err != nil {",
}

// calibrate runs the self-test for one check and reports it. A disagreement is
// an ERROR, never a quiet pass: the caller turns it into exit 2, so a broken
// instrument is distinguishable from a clean corpus.
func calibrate(name string, out io.Writer) error {
	fmt.Fprintf(out, "arm census (%s): instrument calibration\n", name)
	switch name {
	case "tautology":
		for _, c := range tautologyCalibration {
			got, err := tautological(c.pattern, c.syntax)
			if err != nil {
				return fmt.Errorf("calibration %q: %w", c.pattern, err)
			}
			if got != c.want {
				return fmt.Errorf("calibration FAILED: tautological(%q) = %v, want %v — %s. The verdict machinery is broken; a corpus report from it would be meaningless", c.pattern, got, c.want, c.why)
			}
		}
		if _, err := tautological("([unclosed", ""); err == nil {
			return fmt.Errorf("calibration FAILED: an uncompilable pattern was accepted — an unloadable rule must be an error, never a quiet \"not tautological\"")
		}
		if _, err := tautological(".", "pcre"); err == nil {
			return fmt.Errorf("calibration FAILED: an unknown syntax was accepted — the census would be reporting on a corpus the engine cannot run")
		}
		fmt.Fprintf(out, "  probe corpus%22s%d spelling(s) of match-anything caught, %d real token(s) spared — ok\n",
			"", countWant(true), countWant(false))
	case "multi-witness":
		for _, c := range witnessCalibration {
			if got := isLastWitnessShape(c.a); got != c.want {
				return fmt.Errorf("calibration FAILED: isLastWitnessShape(%+v) = %v, want %v — %s. The population this check reports on is no longer the population it is for", c.a, got, c.want, c.why)
			}
		}
		if witnessThreshold < 4 {
			return fmt.Errorf("calibration FAILED: witnessThreshold is %d — below the vacuity census's own DIFFUSE-EVIDENCE cutoff (len(ws) <= 3), so this check would flag arms that census already reasons about, and the repo would carry two definitions of \"diffuse\"", witnessThreshold)
		}
		fmt.Fprintf(out, "  shape classifier%18s%d shape(s), exists and at-least-1 in, explicit n >= 2 out — ok\n", "", len(witnessCalibration))
		fmt.Fprintf(out, "  witness threshold%17s%d — ok\n", "", witnessThreshold)
	case "narrowable":
		for _, c := range narrowableCalibration {
			m, err := compileLine(c.pattern, "")
			if err != nil {
				return fmt.Errorf("calibration %q: %w", c.pattern, err)
			}
			_, got, err := narrowableOn(m, c.line, c.lang)
			if err != nil {
				return fmt.Errorf("calibration %q: %w", c.pattern, err)
			}
			if got != c.want {
				return fmt.Errorf("calibration FAILED: narrowableOn(%q, %q) = %v, want %v — %s. The verdict machinery is broken; a corpus report from it would be meaningless", c.pattern, c.line, got, c.want, c.why)
			}
		}
		if got := traversalSpans("\tfor _, b := range Split(g.Members, country) {", langGo); len(got) != 1 {
			return fmt.Errorf("calibration FAILED: a bracketed range expression yielded %d span(s), want 1 — the narrowing would be spliced into the middle of an argument list", len(got))
		}
		for _, prose := range []string{
			"    // in the same order the workflow declares them",
			"    final label = 'in progress';",
			"    print('cast in place');",
		} {
			if got := traversalSpans(prose, langDart); len(got) != 0 {
				return fmt.Errorf("calibration FAILED: %q yielded %d traversal span(s), want 0 — a bare `in` in prose is not a loop head, and blaming arms for it would flag honest rules", prose, len(got))
			}
		}
		if langGo.narrowing() == "" || langDart.narrowing() == "" || langOther.narrowing() != "" {
			return fmt.Errorf("calibration FAILED: the narrowing suffixes are not the measured ones (go %q, dart %q, other %q)", langGo.narrowing(), langDart.narrowing(), langOther.narrowing())
		}
		fmt.Fprintf(out, "  prefix/terminator table%12s%d flagged shape(s), %d bounded shape(s) spared — ok\n",
			"", countNarrowableWant(true), countNarrowableWant(false))
	case "embedded":
		for _, c := range embeddedCalibration {
			m, err := compileLine(c.pattern, "")
			if err != nil {
				return fmt.Errorf("calibration %q: %w", c.pattern, err)
			}
			_, _, got, err := satisfiedByEmbeddedOnly(m, c.lines)
			if err != nil {
				return fmt.Errorf("calibration %q: %w", c.pattern, err)
			}
			if got != c.want {
				return fmt.Errorf("calibration FAILED: satisfiedByEmbeddedOnly(%q) = %v, want %v — %s. The verdict machinery is broken; a corpus report from it would be meaningless", c.pattern, got, c.want, c.why)
			}
		}
		if tokenAligned("kit.Formula", 1, len("kit.Formula")) {
			return fmt.Errorf("calibration FAILED: `it.Formula` inside `kit.Formula` was called token-aligned — the check reads only one end of the match and spares the shipped spelling")
		}
		if !tokenAligned("it.Formula)", 0, len("it.Formula")) {
			return fmt.Errorf("calibration FAILED: a match bounded by a paren was called embedded — every honest arm would be flagged")
		}
		fmt.Fprintf(out, "  token-alignment table%14s%d flagged shape(s), %d bound or deliberate shape(s) spared — ok\n",
			"", countEmbeddedWant(true), countEmbeddedWant(false))
	default:
		return fmt.Errorf("no calibration for check %q", name)
	}
	return nil
}

// countWant tallies the calibration cases expecting a given verdict, so the
// reported line cannot drift from the table it summarises.
func countWant(want bool) int {
	n := 0
	for _, c := range tautologyCalibration {
		if c.want == want {
			n++
		}
	}
	return n
}

// countNarrowableWant tallies the narrowable calibration cases expecting a
// given verdict, so the reported line cannot drift from the table.
func countNarrowableWant(want bool) int {
	n := 0
	for _, c := range narrowableCalibration {
		if c.want == want {
			n++
		}
	}
	return n
}

// countEmbeddedWant tallies the calibration cases expecting a given verdict, so
// the reported line cannot drift from the table it summarises.
func countEmbeddedWant(want bool) int {
	n := 0
	for _, c := range embeddedCalibration {
		if c.want == want {
			n++
		}
	}
	return n
}
