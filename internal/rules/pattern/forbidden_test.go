package pattern_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func paramsNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func mustChecker(t *testing.T, typeName, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup(typeName)
	if !ok {
		t.Fatalf("type %q not registered", typeName)
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestForbiddenPatternFlagsEachMatchingLine(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "pattern: 'pgxpool\\.New\\('\n")
	f := scan.NewMemFile("db.go", []byte("a\npool := pgxpool.New(ctx)\nb\nagain pgxpool.New(x)\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Line != 2 || ms[1].Line != 4 {
		t.Fatalf("matches: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "pgxpool") {
		t.Fatalf("message: %q", ms[0].Message)
	}
}

// TestForbiddenPlainPatternHonoursPrefilter pins #21: the prefilter gate must
// apply on the plain single-pattern path, not only in all_of/multiline/guarded
// modes. A file matching the pattern but lacking the prefilter literal is
// skipped — the same contract every other mode already honours.
func TestForbiddenPlainPatternHonoursPrefilter(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: 'pgxpool\\.New\\('\nprefilter: material_cat\n")
	f := scan.NewMemFile("db.go", []byte("a\npool := pgxpool.New(ctx)\nb\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("plain-pattern rule must honour its prefilter — file lacks the literal, got %+v", ms)
	}
}

// TestForbiddenPlainPatternPrefilterPresentKeepsEveryLine guards the other
// edge of #21's fix: when the literal IS present the gate must not suppress
// anything — the unguarded plain pattern still flags every matching line.
func TestForbiddenPlainPatternPrefilterPresentKeepsEveryLine(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: 'pgxpool\\.New\\('\nprefilter: material_cat\n")
	f := scan.NewMemFile("db.go", []byte("material_cat\npool := pgxpool.New(ctx)\nb\nagain pgxpool.New(x)\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Line != 2 || ms[1].Line != 4 {
		t.Fatalf("literal present: every matching line must still be reported, got %+v", ms)
	}
}

// TestForbiddenPrefilterWithGuardsBothConsulted pins the combined path: a
// plain rule carrying BOTH a prefilter and require_present must consult each
// gate — a passing prefilter must not skip the guards, and the guards must
// not shadow the prefilter. Every other test exercises exactly one arm, so
// without this pin a break confined to the combination is invisible.
func TestForbiddenPrefilterWithGuardsBothConsulted(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: 'pgxpool\\.New\\('\nprefilter: material_cat\nrequire_present:\n  - 'FromTable'\n")
	// Literal + trigger + required token: exactly one finding, anchored on the
	// first trigger (guarded rules report once per file).
	ms, err := c.CheckFile(scan.NewMemFile("a.go",
		[]byte("material_cat\nFromTable\npool := pgxpool.New(ctx)\nagain pgxpool.New(x)\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 3 {
		t.Fatalf("literal+token+trigger must yield one finding at line 3, got %+v", ms)
	}
	// Literal + trigger, required token absent: the guard must still suppress.
	ms, err = c.CheckFile(scan.NewMemFile("b.go",
		[]byte("material_cat\npool := pgxpool.New(ctx)\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("require_present must gate even when the prefilter passes, got %+v", ms)
	}
	// Trigger + token, literal absent: the prefilter must still suppress.
	ms, err = c.CheckFile(scan.NewMemFile("c.go",
		[]byte("FromTable\npool := pgxpool.New(ctx)\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("prefilter must gate even when the guards pass, got %+v", ms)
	}
}

func TestForbiddenPatternCleanFileNoMatches(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern", "pattern: 'nope'\n")
	ms, err := c.CheckFile(scan.NewMemFile("ok.go", []byte("all\nclean\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("unexpected matches: %+v", ms)
	}
}

// window bounds an all_of co-occurrence to a sliding N-line span: every pattern
// must match within some window of N consecutive lines, not merely somewhere in
// the file. This is the linear replacement for the awk proximity window the
// open-coded-widget gates use (e.g. tone-pill's four pill tokens within 14 lines)
// — the regexp2 port expressed it as nested bounded lookaheads (?=.{0,700}A)…,
// which backtracked. Fires anchored on the earliest token line of the satisfying
// window; stays silent when the tokens are spread beyond the window.
func TestForbiddenAllOfWindowRequiresProximity(t *testing.T) {
	params := "window: 5\nall_of:\n  - 'paddingChip'\n  - 'withValues'\n  - 'borderRadiusSm'\n"
	c := mustChecker(t, "forbidden-pattern", params)
	// All three tokens within a 5-line span (lines 3-5) → fires, anchored on line 3.
	fire := scan.NewMemFile("pill.dart", []byte(
		"a\nb\npaddingChip\nwithValues\nborderRadiusSm\nc\n"))
	ms, err := c.CheckFile(fire)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 3 {
		t.Fatalf("expected one finding anchored on line 3, got %+v", ms)
	}
	// The same three tokens spread beyond the window (lines 3, 4, 20) → no window
	// holds all three → no finding (the 85-lines-apart false positive stays dead).
	spread := make([]byte, 0, 64)
	spread = append(spread, []byte("a\nb\npaddingChip\nwithValues\n")...)
	for i := 0; i < 15; i++ {
		spread = append(spread, []byte("x\n")...)
	}
	spread = append(spread, []byte("borderRadiusSm\n")...)
	ms, err = c.CheckFile(scan.NewMemFile("spread.dart", spread))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("expected no finding when tokens spread beyond the window, got %+v", ms)
	}
}

func TestForbiddenPatternRejectsMissingOrBadPattern(t *testing.T) {
	factory, _ := rules.Lookup("forbidden-pattern")
	if _, err := factory(nil); err == nil {
		t.Fatal("missing pattern accepted")
	}
	if _, err := factory(paramsNode(t, "pattern: '('\n")); err == nil {
		t.Fatal("invalid regex accepted")
	}
}

// require_present gates the line-anchored pattern by whole-file co-occurrence:
// the trigger fires only in a file that ALSO contains every require_present
// pattern. This is the linear-RE2 replacement for a backtracking regexp2
// variable-length lookbehind like (?<=role[\s\S]*)\.broadcast\( — the finding
// still anchors on the trigger line (fixture-compatible), while the guard is a
// single whole-file scan. Mirrors the origin scripts' `grep ctx && grep trigger`.
func TestForbiddenRequirePresentGatesOnFileCooccurrence(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: '\\.broadcast\\('\nrequire_present:\n  - '(?:implements|extends|with)\\s+[A-Za-z]*EventsSource'\n")
	// Trigger present AND the required context present → fires on the trigger line.
	fire := scan.NewMemFile("fake.dart", []byte(
		"class FakeSource implements DockIntakeEventsSource {\n  final c = StreamController.broadcast();\n}\n"))
	ms, err := c.CheckFile(fire)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 2 {
		t.Fatalf("expected one finding on line 2, got %+v", ms)
	}
	// Trigger present but the required context ABSENT → no finding.
	pass := scan.NewMemFile("plain.dart", []byte(
		"class Widget {\n  final c = StreamController.broadcast();\n}\n"))
	ms, err = c.CheckFile(pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("expected no finding when required context absent, got %+v", ms)
	}
}

// A guarded pattern is a FILE-LEVEL co-occurrence assertion (the origin scripts
// are all `grep -q ctx && grep -q trigger`), so it fires once per file — the
// first trigger occurrence — not once per occurrence the way an unguarded
// forbidden-pattern does. Anchoring on the first match keeps parity with the
// origin's single file-level finding (e.g. a codec that writes both '[' and ']').
func TestForbiddenGuardedPatternFiresOnceOnFirstMatch(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: '(?:''\\[''|''\\]'')'\nrequire_present:\n  - 'strconv\\.FormatFloat\\('\n")
	f := scan.NewMemFile("codec.go", []byte(
		"func f() {\n\tb.WriteByte('[')\n\tx := strconv.FormatFloat(v)\n\tb.WriteByte(']')\n}\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 2 {
		t.Fatalf("expected exactly one finding on the first trigger line (2), got %+v", ms)
	}
}

// require_absent is the negative guard: the trigger fires only in a file that
// contains NONE of the require_absent patterns. Linear-RE2 replacement for a
// negative lookahead like (?![\s\S]*allowed).
func TestForbiddenRequireAbsentGatesOnFileCooccurrence(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: 'rawQuery'\nrequire_absent:\n  - 'GENERATED-OK'\n")
	fire := scan.NewMemFile("a.go", []byte("x\nrawQuery(sql)\n"))
	ms, err := c.CheckFile(fire)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 2 {
		t.Fatalf("expected finding on line 2, got %+v", ms)
	}
	exempt := scan.NewMemFile("b.go", []byte("// GENERATED-OK\nrawQuery(sql)\n"))
	ms, err = c.CheckFile(exempt)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("expected no finding when require_absent token present, got %+v", ms)
	}
}

func TestForbiddenWithoutPrefilterUngates(t *testing.T) {
	c := mustChecker(t, "forbidden-pattern",
		"pattern: 'Category = \"[a-z]+\"'\nprefilter: material_cat\nmultiline: true\n")

	// Matches the pattern but LACKS the prefilter literal.
	f := scan.NewMemFile("consts.go", []byte("package p\nvar Category = \"floor\"\n"))

	base, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != 0 {
		t.Fatalf("gated checker must skip a file lacking the prefilter literal: %+v", base)
	}

	p, ok := c.(rules.Prefiltered)
	if !ok {
		t.Fatal("forbidden must implement rules.Prefiltered")
	}
	if p.Prefilter() != "material_cat" {
		t.Fatalf("Prefilter() = %q, want material_cat", p.Prefilter())
	}

	stripped, err := p.WithoutPrefilter().CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(stripped) != 1 || stripped[0].Line != 2 {
		t.Fatalf("ungated checker must match at line 2: %+v", stripped)
	}
}
