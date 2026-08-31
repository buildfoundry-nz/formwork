package main

import (
	"strings"
	"testing"
)

// TestTautologicalRecognisesEverySpelling is the reason the verdict is
// empirical rather than a blocklist. Each pattern here is "match any line"
// written differently — including the two zero-width spellings, which a
// blocklist of `.`/`.*`/`.+`/`[\s\S]*` would wave straight through.
func TestTautologicalRecognisesEverySpelling(t *testing.T) {
	for _, pat := range []string{
		".",
		".*",
		".+",
		"[\\s\\S]*",
		"[\\s\\S]+",
		"(?s).*",
		".{0,}",
		".{1,}",
		"[a-z]*", // zero-width: matches every line through its empty match
		"x?",     // same
		"^",
		"\\s|\\S",
	} {
		got, err := tautological(pat, "")
		if err != nil {
			t.Fatalf("tautological(%q): %v", pat, err)
		}
		if !got {
			t.Errorf("pattern %q matches an arbitrary line but was not flagged", pat)
		}
	}
}

// TestTautologicalLeavesRealPatternsAlone pins the other half: a pattern that
// names a token must never be flagged. A false positive here retires a live
// gate, which is worse than the vacuity the check exists to remove.
func TestTautologicalLeavesRealPatternsAlone(t *testing.T) {
	for _, pat := range []string{
		`WithTenantOrg\(`,
		`^CREATE TABLE`,
		`USING btree`,
		`^\s*assert_[a-z_]+ `,
		`takeoffqs\.dev/schema/gen/go/`,
		`^[^#]*file-size-caps\.test\.sh`, // comment-immune, but still pins a token
		`Path:\s*"`,
		`^$`, // matches only an empty line — restrictive, not tautological
	} {
		got, err := tautological(pat, "")
		if err != nil {
			t.Fatalf("tautological(%q): %v", pat, err)
		}
		if got {
			t.Errorf("pattern %q pins a real token but was flagged as tautological", pat)
		}
	}
}

// TestTautologicalHonoursRegexp2Syntax — the corpus carries regexp2 arms, and
// compiling one as RE2 would error out on the lookaround and take the whole
// census with it.
func TestTautologicalHonoursRegexp2Syntax(t *testing.T) {
	got, err := tautological(`(?s).*`, "regexp2")
	if err != nil {
		t.Fatalf("regexp2 tautology: %v", err)
	}
	if !got {
		t.Error("regexp2 `(?s).*` not flagged")
	}
	got, err = tautological(`claddinggrouptotals\.(List|Queue)\((?![^()]*,\s*nil\))`, "regexp2")
	if err != nil {
		t.Fatalf("regexp2 real pattern: %v", err)
	}
	if got {
		t.Error("a regexp2 lookaround pattern was flagged as tautological")
	}
}

// TestTautologicalFailsClosedOnBadInput — an uncompilable pattern or an unknown
// syntax is an ERROR, never a quiet "not tautological". The engine refuses to
// load such a rule, so a census that swallowed it would be reporting on a
// corpus the gate cannot run.
func TestTautologicalFailsClosedOnBadInput(t *testing.T) {
	if _, err := tautological("([unclosed", ""); err == nil {
		t.Error("uncompilable pattern accepted")
	}
	if _, err := tautological(".", "pcre"); err == nil {
		t.Error("unknown syntax accepted")
	}
}

// TestLoadCorpusReadsParamsPerArm pins that params come from a real YAML decode
// and that each arm's line range is its own. A shifted range attaches one arm's
// verdict to its neighbour's line number.
func TestLoadCorpusReadsParamsPerArm(t *testing.T) {
	arms, err := loadCorpus("testdata/taut-fire-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 2 {
		t.Fatalf("arms: %d, want 2", len(arms))
	}
	if arms[0].ID != "demo-tautological" || arms[0].Pattern != "." || arms[0].Mode != "every-file" {
		t.Errorf("arm 0: %+v", arms[0])
	}
	if arms[1].ID != "demo-honest" || arms[1].Pattern != `WithTenantOrg\(` || arms[1].Mode != "exists" {
		t.Errorf("arm 1: %+v", arms[1])
	}
	if arms[0].Line >= arms[1].Line {
		t.Errorf("arm lines not in declaration order: %d, %d", arms[0].Line, arms[1].Line)
	}
	if arms[0].File != ".formwork/rules/demo.yaml" {
		t.Errorf("file: %q", arms[0].File)
	}
}

func TestDetectTautologiesFlagsOnlyTheTautologicalArm(t *testing.T) {
	arms, err := loadCorpus("testdata/taut-fire-1")
	if err != nil {
		t.Fatal(err)
	}
	bad, examined, err := detectTautologies(arms)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 2 {
		t.Errorf("examined %d required-pattern arms, want 2", examined)
	}
	if len(bad) != 1 || bad[0].arm != "demo-tautological" {
		t.Fatalf("flagged: %+v", bad)
	}
}

// TestDetectTautologiesIgnoresForbiddenPatterns — a forbidden `.` bans every
// non-empty file. That is a rule that always FAILS; it announces itself and is
// not this defect.
func TestDetectTautologiesIgnoresForbiddenPatterns(t *testing.T) {
	arms, err := loadCorpus("testdata/taut-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	bad, _, err := detectTautologies(arms)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Fatalf("flagged: %+v", bad)
	}
}

func TestIsLastWitnessShape(t *testing.T) {
	cases := []struct {
		a    arm
		want bool
	}{
		{arm{Type: "required-pattern", Mode: "exists"}, true},
		{arm{Type: "pattern-count", Op: "at-least", N: 1}, true},
		// An explicit n >= 2 is a stated cardinality, not the accidental floor
		// of one that `exists` confers — tqs-package-consumers-tqs-core is
		// `at-least 2` over 42 witnesses because the invariant IS ">= 2
		// consumers" (#6858).
		{arm{Type: "pattern-count", Op: "at-least", N: 2}, false},
		{arm{Type: "pattern-count", Op: "at-least", N: 517}, false},
		{arm{Type: "required-pattern", Mode: "every-file"}, false},
		{arm{Type: "required-pattern", Mode: ""}, false}, // every-file is the default
		{arm{Type: "forbidden-pattern"}, false},
		{arm{Type: "pattern-count", Op: "at-most", N: 1}, false},
		{arm{Type: "command"}, false},
	}
	for _, c := range cases {
		if got := isLastWitnessShape(c.a); got != c.want {
			t.Errorf("isLastWitnessShape(%+v) = %v, want %v", c.a, got, c.want)
		}
	}
}

// TestDetectMultiWitnessFlagsLastWitnessGate is the core verdict: 14 witnesses
// behind a floor of one. It also pins that the count is DECOMMENTED — the
// fixture's 15th match is a comment, and a census that counted prose would
// report 15 and print the wrong n in its cure.
func TestDetectMultiWitnessFlagsLastWitnessGate(t *testing.T) {
	root := "testdata/mw-fire-1"
	arms, err := loadCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	bad, examined, err := detectMultiWitness(root, arms)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 1 {
		t.Errorf("examined %d, want 1", examined)
	}
	if len(bad) != 1 || bad[0].arm != "demo-last-witness" {
		t.Fatalf("flagged: %+v", bad)
	}
	if !strings.Contains(bad[0].detail, "14 witnesses") {
		t.Errorf("detail should report the decommented count of 14: %q", bad[0].detail)
	}
	if !strings.Contains(bad[0].detail, "n: 14") {
		t.Errorf("detail should print the n to write: %q", bad[0].detail)
	}
}

// TestDetectMultiWitnessPassesCuredAndDesignThresholdArms — the cured arm
// (at-least 14) and the stated design threshold (at-least 2) sit over the SAME
// 14 witnesses, and neither may be flagged. The first is the fix; the second is
// what makes the check safe to turn on.
func TestDetectMultiWitnessPassesCuredAndDesignThresholdArms(t *testing.T) {
	root := "testdata/mw-pass-1"
	arms, err := loadCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	bad, examined, err := detectMultiWitness(root, arms)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 0 {
		t.Errorf("examined %d, want 0 — neither arm is last-witness shaped", examined)
	}
	if len(bad) != 0 {
		t.Fatalf("flagged: %+v", bad)
	}
}

// TestRunExitCodes pins the contract the formwork `command` rule reads:
// 0 clean, 1 offenders, 2 usage/env error.
func TestRunExitCodes(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(checks["tautology"], "testdata/taut-fire-1", &out, &errOut); code != 1 {
		t.Errorf("offending corpus exit = %d, want 1\n%s", code, out.String())
	}
	out.Reset()
	if code := run(checks["tautology"], "testdata/taut-pass-1", &out, &errOut); code != 0 {
		t.Errorf("clean corpus exit = %d, want 0\n%s", code, out.String())
	}
	out.Reset()
	if code := run(checks["multi-witness"], "testdata/does-not-exist", &out, &errOut); code != 2 {
		t.Errorf("unreadable root exit = %d, want 2", code)
	}
}

// TestLoadCorpusIsNotKeyOrderDependent — arms are located by the YAML node, not
// by scanning for a `- id:` line. `id` first is a convention of how this corpus
// happens to be written, not a property of YAML, and it does not hold in the
// one place the census is most load-bearing: mutation-proof re-marshals each
// rule into a scratch corpus (prove.go), and yaml.Marshal emits map keys
// ALPHABETICALLY, so `cure:` opens the arm. A text-scanning reader finds zero
// arm headers there and reports on a corpus it never read.
func TestLoadCorpusIsNotKeyOrderDependent(t *testing.T) {
	arms, err := loadCorpus("testdata/taut-keyorder-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 2 {
		t.Fatalf("arms: %d, want 2", len(arms))
	}
	if arms[0].ID != "demo-tautological" || arms[0].Pattern != "." || arms[0].Mode != "every-file" {
		t.Errorf("arm 0: %+v", arms[0])
	}
	if arms[1].ID != "demo-honest" || arms[1].Pattern != `WithTenantOrg\(` {
		t.Errorf("arm 1: %+v", arms[1])
	}
	if arms[0].Line >= arms[1].Line {
		t.Errorf("arm lines not in declaration order: %d, %d", arms[0].Line, arms[1].Line)
	}
	bad, examined, err := detectTautologies(arms)
	if err != nil {
		t.Fatal(err)
	}
	if examined != 2 {
		t.Errorf("examined %d, want 2", examined)
	}
	if len(bad) != 1 || bad[0].arm != "demo-tautological" {
		t.Fatalf("flagged: %+v", bad)
	}
}

// TestCalibrationHoldsOnTheRealInstrument — the self-test must pass for both
// checks as shipped. It is what makes a broken detector loud instead of green,
// and it is the only thing a mutation can bite in mutation-proof's PRUNED
// scratch corpus, where there is no offender left to detect.
func TestCalibrationHoldsOnTheRealInstrument(t *testing.T) {
	for _, name := range []string{"tautology", "multi-witness", "narrowable"} {
		var out strings.Builder
		if err := calibrate(name, &out); err != nil {
			t.Fatalf("calibrate(%s): %v", name, err)
		}
	}
}

// TestCalibrationCatchesAnInvertedProbe — the tautology verdict inverted. Every
// honest pattern would be flagged and every tautological one spared; the
// calibration must refuse to report a corpus at all.
func TestCalibrationCatchesAnInvertedProbe(t *testing.T) {
	orig := tautologyCalibration
	t.Cleanup(func() { tautologyCalibration = orig })
	tautologyCalibration = []calibrationCase{{".", "", false, "inverted expectation stands in for an inverted probe"}}
	var out strings.Builder
	if err := calibrate("tautology", &out); err == nil {
		t.Fatal("calibration accepted a verdict it disagrees with")
	}
}

// TestCalibrationRejectsAnUnknownCheck — a renamed check must be an error, not
// a silently un-calibrated run.
func TestCalibrationRejectsAnUnknownCheck(t *testing.T) {
	var out strings.Builder
	if err := calibrate("no-such-check", &out); err == nil {
		t.Fatal("an unknown check was calibrated")
	}
}
