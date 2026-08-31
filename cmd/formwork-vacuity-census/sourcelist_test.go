package main

import (
	"strings"
	"testing"
)

// decl builds one source-list declaration for the arm under test.
func decl(ruleID, dir string, globs ...string) sourceListDecl {
	return sourceListDecl{ruleID: ruleID, dir: dir, globs: globs}
}

// codes flattens the verdict codes a rule drew, so a case asserts on what the
// arm SAID rather than on the order it happened to say it in.
func codes(t *testing.T, out map[string][]verdict, ruleID string) []string {
	t.Helper()
	var cs []string
	for _, v := range out[ruleID] {
		cs = append(cs, v.code)
	}
	return cs
}

// evidenceOf joins every verdict's evidence for a rule into one searchable
// string — the arm must NAME the drifted file, not merely count it.
func evidenceOf(out map[string][]verdict, ruleID string) string {
	var b strings.Builder
	for _, v := range out[ruleID] {
		b.WriteString(v.why)
		for _, e := range v.evidence {
			b.WriteString("\n" + e)
		}
	}
	return b.String()
}

// The #13517 defect, stated as a test: a source lands in the package and the
// hand-written list is not updated. It matches no glob, so no other arm in this
// census can see it.
func TestSourceList_FiresOnUnlistedSource(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/census",
			"tools/census/main.go",
			"tools/census/classify.go"),
	}}
	paths := []string{
		"tools/census/main.go",
		"tools/census/classify.go",
		"tools/census/scopeglobs.go",
	}

	out := sourceListVerdicts(gm, paths)

	if got := codes(t, out, "census"); len(got) != 1 || got[0] != "SOURCE-UNLISTED" {
		t.Fatalf("verdict codes = %v, want exactly [SOURCE-UNLISTED] — a source present in the "+
			"declared directory and absent from scope.include is the whole defect", got)
	}
	if ev := evidenceOf(out, "census"); !strings.Contains(ev, "tools/census/scopeglobs.go") {
		t.Errorf("the finding does not name the unlisted source:\n%s", ev)
	}
	if ev := evidenceOf(out, "census"); strings.Contains(ev, "tools/census/main.go") {
		t.Errorf("the finding names a source that IS listed — only the missing one is the "+
			"finding, or a reader cannot tell which row to add:\n%s", ev)
	}
}

// Every source listed. Without this control the case above would be satisfied by
// an arm that fires on every rule carrying the marker.
func TestSourceList_PassesWhenListIsComplete(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/census",
			"tools/census/main.go",
			"tools/census/classify.go"),
	}}
	paths := []string{"tools/census/main.go", "tools/census/classify.go"}

	if out := sourceListVerdicts(gm, paths); len(out) != 0 {
		t.Fatalf("arm fired on a complete list: %v", out)
	}
}

// A rule's sources are what it is BUILT from. Its tests are not, so a new
// *_test.go must not oblige a scope.include row — that would make the marker a
// tax on writing tests, and the census's own package gains them constantly.
func TestSourceList_IgnoresTestFiles(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/census", "tools/census/main.go"),
	}}
	paths := []string{
		"tools/census/main.go",
		"tools/census/main_test.go",
		"tools/census/scopeindex_test.go",
	}

	if out := sourceListVerdicts(gm, paths); len(out) != 0 {
		t.Fatalf("arm demanded a scope.include row for a _test.go file: %v", out)
	}
}

// A Go package is ONE directory. A source in a nested directory belongs to a
// different package, so requiring it here would make the marker fire on any
// tool that grows a subpackage.
func TestSourceList_IgnoresNestedDirectories(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/census", "tools/census/main.go"),
	}}
	paths := []string{
		"tools/census/main.go",
		"tools/census/internal/helper.go",
		"tools/census/live-include-globs/rows.go",
	}

	if out := sourceListVerdicts(gm, paths); len(out) != 0 {
		t.Fatalf("arm reached into a nested directory: %v", out)
	}
}

// Coverage is decided with formwork's own matcher, so a list that names its
// directory with a glob is complete. The claim the marker makes is that nothing
// is MISSING — not that the list is spelled one particular way.
func TestSourceList_GlobEntryCoversTheDirectory(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/census", "tools/census/*.go"),
	}}
	paths := []string{"tools/census/main.go", "tools/census/classify.go"}

	if out := sourceListVerdicts(gm, paths); len(out) != 0 {
		t.Fatalf("arm flagged sources a declared glob already covers: %v", out)
	}
}

// The arm's own vacuity guard. A marker over a directory the walk yields nothing
// for gates nothing — and reads as coverage, which is the exact failure this
// census exists to catch one level up. The two live causes are a typo and a path
// under .formwork/, which scan.Walk drops before any rule runs.
func TestSourceList_FiresWhenDeclaredDirectoryHasNoSources(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/typo", "tools/census/main.go"),
	}}
	paths := []string{"tools/census/main.go"}

	out := sourceListVerdicts(gm, paths)
	if got := codes(t, out, "census"); len(got) != 1 || got[0] != "SOURCE-LIST-VACUOUS" {
		t.Fatalf("verdict codes = %v, want exactly [SOURCE-LIST-VACUOUS] — a declaration over an "+
			"empty set must not pass silently", got)
	}
	if ev := evidenceOf(out, "census"); !strings.Contains(ev, "tools/typo") {
		t.Errorf("the finding does not name the declared directory:\n%s", ev)
	}
}

// A directory holding only test files is the same vacuity: nothing the list
// could be exhaustive OVER.
func TestSourceList_FiresWhenDirectoryHoldsOnlyTests(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("census", "tools/census", "tools/census/main.go"),
	}}
	paths := []string{"tools/census/main_test.go"}

	got := codes(t, sourceListVerdicts(gm, paths), "census")
	if len(got) != 1 || got[0] != "SOURCE-LIST-VACUOUS" {
		t.Fatalf("verdict codes = %v, want exactly [SOURCE-LIST-VACUOUS]", got)
	}
}

// Opt-in is real: a corpus where no rule declared a source list draws no
// verdicts, whatever the tree looks like. Every arm here is subscribed to, never
// imposed.
func TestSourceList_NoDeclarationDrawsNoVerdicts(t *testing.T) {
	gm := &globMeasure{}
	paths := []string{"tools/census/main.go", "tools/census/scopeglobs.go"}

	if got := sourceListVerdicts(gm, paths); len(got) != 0 {
		t.Fatalf("arm fired without any rule declaring a source list: %v", got)
	}
}

// Every verdict this census emits gates (#12178): a detected class credited as
// coverage while enforcing nothing is the same failure the census exists to
// catch. A non-gating verdict must be written and reviewed as a deliberate
// false, never arrived at by leaving a field unset.
func TestSourceList_VerdictsGate(t *testing.T) {
	gm := &globMeasure{sourceLists: []sourceListDecl{
		decl("unlisted", "tools/census", "tools/census/main.go"),
		decl("vacuous", "tools/gone", "tools/gone/main.go"),
	}}
	paths := []string{"tools/census/main.go", "tools/census/drifted.go"}

	for id, vs := range sourceListVerdicts(gm, paths) {
		if len(vs) == 0 {
			t.Errorf("rule %s has an empty verdict slice", id)
		}
		for _, v := range vs {
			if !v.gating {
				t.Errorf("rule %s verdict %s does not gate", id, v.code)
			}
			if v.class != class1Glob {
				t.Errorf("rule %s verdict %s is class %q, want %q — this is scope-declaration "+
					"integrity, the class GLOB-UNTRACKED already lives in", id, v.code, v.class, class1Glob)
			}
			if strings.TrimSpace(v.why) == "" {
				t.Errorf("rule %s verdict %s carries no why", id, v.code)
			}
		}
	}
}

// The marker is read off the comment plane, on the scope.include KEY. These pin
// the parse itself, which no verdict case can reach: a marker that is silently
// not recognised makes every arm above pass vacuously.
func TestParseRuleGlobs_ReadsSourceListMarker(t *testing.T) {
	const y = `rules:
  - id: census
    type: forbidden-pattern
    scope:
      # source-list-exhaustive: tools/census
      include:
        - "tools/census/main.go"
`
	_, decls, err := parseRuleGlobs([]byte(y))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(decls) != 1 {
		t.Fatalf("got %d declaration(s), want 1", len(decls))
	}
	if decls[0].ruleID != "census" || decls[0].dir != "tools/census" {
		t.Errorf("declaration = %+v, want rule census over tools/census", decls[0])
	}
	if len(decls[0].globs) != 1 || decls[0].globs[0] != "tools/census/main.go" {
		t.Errorf("declaration carries globs %v, want the include list verbatim", decls[0].globs)
	}
}

// A bare marker declares nothing — the same non-empty-reason convention
// `# glob-dead:` uses. Without this, a truncated edit would subscribe a rule to
// an arm keyed on an empty directory.
func TestParseRuleGlobs_BareSourceListMarkerDoesNotSubscribe(t *testing.T) {
	const y = `rules:
  - id: census
    type: forbidden-pattern
    scope:
      # source-list-exhaustive:
      include:
        - "tools/census/main.go"
`
	_, decls, err := parseRuleGlobs([]byte(y))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(decls) != 0 {
		t.Fatalf("a bare marker subscribed the rule: %+v", decls)
	}
}

// Anchored to the KEY, not to an entry. A marker sitting above a glob is a claim
// about that glob, which this vocabulary does not make — it must not be read as
// a whole-list declaration.
func TestParseRuleGlobs_SourceListMarkerOnAnEntryIsNotADeclaration(t *testing.T) {
	const y = `rules:
  - id: census
    type: forbidden-pattern
    scope:
      include:
        # source-list-exhaustive: tools/census
        - "tools/census/main.go"
`
	_, decls, err := parseRuleGlobs([]byte(y))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(decls) != 0 {
		t.Fatalf("a marker on an entry was read as a whole-list declaration: %+v", decls)
	}
}

// A corpus with no marker anywhere must yield no declarations — the negative
// control for the parse, matching the opt-in case for the arm.
func TestParseRuleGlobs_NoMarkerYieldsNoDeclaration(t *testing.T) {
	const y = `rules:
  - id: census
    type: forbidden-pattern
    scope:
      include:
        - "tools/census/main.go"
`
	_, decls, err := parseRuleGlobs([]byte(y))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(decls) != 0 {
		t.Fatalf("got declarations from a corpus with no marker: %+v", decls)
	}
}
