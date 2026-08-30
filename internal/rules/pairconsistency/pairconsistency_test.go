package pairconsistency_test

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

func mustChecker(t *testing.T, params string) rules.Checker {
	t.Helper()
	factory, ok := rules.Lookup("pair-consistency")
	if !ok {
		t.Fatal("type \"pair-consistency\" not registered")
	}
	c, err := factory(paramsNode(t, params))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPairConsistencyTriggerWithCompanionPasses(t *testing.T) {
	c := mustChecker(t, "trigger: 'BeginTx\\('\nrequires: 'Commit\\(|Rollback\\('\n")
	f := scan.NewMemFile("tx.go", []byte("a\ntx := db.BeginTx(ctx)\ndefer tx.Rollback()\nb\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("file with both trigger and companion flagged: %+v", ms)
	}
}

func TestPairConsistencyTriggerWithoutCompanionFails(t *testing.T) {
	c := mustChecker(t, "trigger: 'BeginTx\\('\nrequires: 'Commit\\(|Rollback\\('\n")
	f := scan.NewMemFile("tx.go", []byte("a\nb\ntx := db.BeginTx(ctx)\nc\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want one match, got %+v", ms)
	}
	if ms[0].Line != 3 {
		t.Fatalf("want match at first trigger line 3, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "Commit") {
		t.Fatalf("message does not name the missing companion: %q", ms[0].Message)
	}
}

func TestPairConsistencyNeitherMatchesPasses(t *testing.T) {
	c := mustChecker(t, "trigger: 'BeginTx\\('\nrequires: 'Commit\\('\n")
	ms, err := c.CheckFile(scan.NewMemFile("clean.go", []byte("all\nclean\nhere\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("file with neither trigger nor companion flagged: %+v", ms)
	}
}

func TestPairConsistencyReportsFirstTriggerLine(t *testing.T) {
	c := mustChecker(t, "trigger: 'open'\nrequires: 'close'\n")
	f := scan.NewMemFile("res.txt", []byte("noise\nopen one\nmore\nopen two\n"))
	ms, err := c.CheckFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Line != 2 {
		t.Fatalf("want single match at first trigger line 2, got %+v", ms)
	}
}

func TestPairConsistencyDefaultWhereIsSameFile(t *testing.T) {
	// where omitted must behave identically to where: same-file.
	c := mustChecker(t, "trigger: 't'\nrequires: 'r'\nwhere: same-file\n")
	if _, err := c.CheckFile(scan.NewMemFile("x.txt", []byte("t\nr\n"))); err != nil {
		t.Fatal(err)
	}
}

func TestPairConsistencyRejectsBadWhere(t *testing.T) {
	factory, _ := rules.Lookup("pair-consistency")
	_, err := factory(paramsNode(t, "trigger: t\nrequires: r\nwhere: cross-file\n"))
	if err == nil {
		t.Fatal("unsupported where accepted")
	}
	// Strict decoding is only useful if the error tells the author what IS
	// accepted — same-file, same-dir, and same-func (#9767) must all be named.
	for _, want := range []string{"same-file", "same-dir", "same-func"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("unknown-where error %q does not name the accepted value %q", err, want)
		}
	}
}

func TestPairConsistencyRejectsMissingOrBadParams(t *testing.T) {
	factory, _ := rules.Lookup("pair-consistency")
	if _, err := factory(paramsNode(t, "requires: r\n")); err == nil {
		t.Fatal("missing trigger accepted")
	}
	if _, err := factory(paramsNode(t, "trigger: t\n")); err == nil {
		t.Fatal("missing requires accepted")
	}
	if _, err := factory(paramsNode(t, "trigger: '('\nrequires: r\n")); err == nil {
		t.Fatal("invalid trigger regex accepted")
	}
	if _, err := factory(paramsNode(t, "trigger: t\nrequires: '('\n")); err == nil {
		t.Fatal("invalid requires regex accepted")
	}
	if _, err := factory(paramsNode(t, "trigger: t\nrequires: r\nbogus: 1\n")); err == nil {
		t.Fatal("unknown param field accepted")
	}
}

// --- where: same-dir -------------------------------------------------------
//
// same-dir widens the unit from one file to the containing DIRECTORY — the
// package, for Go. Some invariants are only expressible that way: the
// motivating case is a package that satisfies the contract collectively, one
// file assigning a value and a shared tail in a sibling file answering with it
// (the derived-dispatch-claims-ledger shape in examples/palletra-port-full,
// where the auto-run engine enqueues in one file and claims in another). A
// same-file rule flags the assigning file and so condemns a package that is
// correct; papering over that with a scope exclude would be a carve-out, not a
// lockdown.

// wantMatch is the (path, line) a same-dir finding must be anchored on.
type wantMatch struct {
	path string
	line int
}

// runDirScope feeds files through CheckFile in the given order — as the
// engine's phase-1 worker pool does, in an order the rule may not assume — then
// runs the phase-2 finalizer, resolving each match's path the way the engine
// does.
func runDirScope(t *testing.T, c rules.Checker, files map[string]string, order []string) []rules.Match {
	t.Helper()
	var out []rules.Match
	for _, path := range order {
		ms, err := c.CheckFile(scan.NewMemFile(path, []byte(files[path])))
		if err != nil {
			t.Fatalf("CheckFile(%s): %v", path, err)
		}
		for _, m := range ms {
			if m.Path == "" {
				m.Path = path
			}
			out = append(out, m)
		}
	}
	fin, ok := c.(rules.Finalizer)
	if !ok {
		t.Fatal("pair-consistency must implement Finalizer to serve where: same-dir")
	}
	out = append(out, fin.Finalize()...)
	return out
}

func TestPairConsistencySameDirUnitIsTheDirectory(t *testing.T) {
	const sameDirParams = "trigger: 'assignDelta'\nrequires: 'writeFresh'\nwhere: same-dir\n"

	cases := []struct {
		name  string
		files map[string]string
		order []string
		want  []wantMatch
	}{
		{
			name: "companion in a sibling file satisfies the package",
			files: map[string]string{
				"routes/tuning/tuning_apply.go": "assignDelta()\n",
				"routes/tuning/tuning.go":       "writeFresh()\n",
			},
			order: []string{"routes/tuning/tuning_apply.go", "routes/tuning/tuning.go"},
		},
		{
			name: "trigger and companion in one file still passes",
			files: map[string]string{
				"routes/solo/solo.go": "assignDelta()\nwriteFresh()\n",
			},
			order: []string{"routes/solo/solo.go"},
		},
		{
			name: "package with no companion anywhere fires, anchored on the trigger line",
			files: map[string]string{
				"routes/broken/handler.go": "package broken\nassignDelta()\n",
				"routes/broken/helper.go":  "package broken\n",
			},
			order: []string{"routes/broken/handler.go", "routes/broken/helper.go"},
			want:  []wantMatch{{path: "routes/broken/handler.go", line: 2}},
		},
		{
			name: "a companion in a DIFFERENT directory does not satisfy the trigger's",
			files: map[string]string{
				"routes/a/a.go": "assignDelta()\n",
				"routes/b/b.go": "writeFresh()\n",
			},
			order: []string{"routes/a/a.go", "routes/b/b.go"},
			want:  []wantMatch{{path: "routes/a/a.go", line: 1}},
		},
		{
			name: "a package that does neither is untouched",
			files: map[string]string{
				"routes/quiet/quiet.go": "package quiet\n",
			},
			order: []string{"routes/quiet/quiet.go"},
		},
		{
			name: "two offending packages report both, ordered by directory",
			files: map[string]string{
				"routes/zulu/z.go":  "assignDelta()\n",
				"routes/alpha/a.go": "assignDelta()\n",
			},
			order: []string{"routes/zulu/z.go", "routes/alpha/a.go"},
			want: []wantMatch{
				{path: "routes/alpha/a.go", line: 1},
				{path: "routes/zulu/z.go", line: 1},
			},
		},
		{
			name: "anchor is the first triggering file in PATH order, not visit order",
			files: map[string]string{
				"routes/multi/a_first.go": "\nassignDelta()\n",
				"routes/multi/z_last.go":  "assignDelta()\n",
			},
			// Visited last-first: the worker pool imposes no order, so the
			// reported anchor must not depend on one.
			order: []string{"routes/multi/z_last.go", "routes/multi/a_first.go"},
			want:  []wantMatch{{path: "routes/multi/a_first.go", line: 2}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runDirScope(t, mustChecker(t, sameDirParams), tc.files, tc.order)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d matches %+v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				if got[i].Path != w.path || got[i].Line != w.line {
					t.Errorf("match %d = %s:%d, want %s:%d", i, got[i].Path, got[i].Line, w.path, w.line)
				}
				if !strings.Contains(got[i].Message, "directory") {
					t.Errorf("match %d message %q should name the directory unit so the cure is unambiguous", i, got[i].Message)
				}
			}
		})
	}
}

// TestPairConsistencySameDirEmitsNothingPerFile pins where the same-dir verdict
// is reached: the per-file pass only accumulates evidence, so no finding can
// escape before every file of the directory has been seen.
func TestPairConsistencySameDirEmitsNothingPerFile(t *testing.T) {
	c := mustChecker(t, "trigger: 'assignDelta'\nrequires: 'writeFresh'\nwhere: same-dir\n")
	ms, err := c.CheckFile(scan.NewMemFile("routes/broken/handler.go", []byte("assignDelta()\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("same-dir must not judge a file in isolation, got %+v", ms)
	}
}

// TestPairConsistencyWholeTreeInvariantOnlyInSameDirMode pins the #4 mechanism:
// a same-dir verdict joins evidence across the files of a directory, so a
// changeset-restricted scan (--staged / --range) that holds the trigger's file
// while its companion sits in an untouched sibling would false-fail every
// commit that touches the trigger. It therefore evaluates whole-tree, joining
// set-relation / pattern-count / baseline / required-pattern(exists).
// same-file is a per-file assertion and stays range-scopeable.
func TestPairConsistencyWholeTreeInvariantOnlyInSameDirMode(t *testing.T) {
	sameDir := mustChecker(t, "trigger: 'a'\nrequires: 'b'\nwhere: same-dir\n")
	if !rules.IsWholeTreeInvariant(sameDir) {
		t.Error("where: same-dir must be a whole-tree invariant — its verdict joins files, " +
			"so a changeset-restricted scan false-fails whenever the companion file is untouched")
	}
	for _, params := range []string{
		"trigger: 'a'\nrequires: 'b'\n",
		"trigger: 'a'\nrequires: 'b'\nwhere: same-file\n",
		"trigger: 'a'\nrequires: 'b'\nwhere: same-func\n",
	} {
		if rules.IsWholeTreeInvariant(mustChecker(t, params)) {
			t.Errorf("where: same-file/same-func (%q) is a per-file assertion and must stay range-scopeable", params)
		}
	}
}

// --- where: same-func ------------------------------------------------------
//
// #9767: the retired shell gates walked function bodies with a brace-depth
// accumulator that closed on the first zero-depth line. A multi-line signature
// has no `{` yet, so depth stayed 0 and evaluate() saw only the signature —
// the body (and every invariant the gate claimed to guard) was invisible.
// same-func uses go/parser function spans so multi-line signatures, brace-
// bearing parameter types, and nested Memo closures are in scope by construction.

const sameFuncParams = "trigger: 'projectMetricUnionSQL'\nrequires: 'projectreadscope\\.Memo\\('\nwhere: same-func\n"

// multiLineBare is the #9767 reproduction shape: multi-line signature, trigger
// + Query in the body, no Memo. same-file would also catch a file that ONLY
// contains this, but same-func is what keeps it caught when a sibling func
// already has Memo (see greenwash below).
const multiLineBare = `package p

func loadAnnotationMetricsForQueriesAndPages(
	ctx interface{},
	tx interface{},
	projectID string,
) error {
	_ = projectMetricUnionSQL // want
	tx.Query()
	return nil
}
`

// TestPairConsistencySameFuncMultiLineSignatureFires is A3: a multi-line
// signature must not blind the unit. Fails today if same-func is rejected or
// implemented as same-file-with-broken-accumulator.
func TestPairConsistencySameFuncMultiLineSignatureFires(t *testing.T) {
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(multiLineBare)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("multi-line signature bare body must fire once, got %+v", ms)
	}
	if ms[0].Line != 8 {
		t.Fatalf("finding must anchor on the trigger line inside the func (line 8), not the decl line, got line %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "loadAnnotationMetricsForQueriesAndPages") {
		t.Fatalf("message must name the offending func, got %q", ms[0].Message)
	}
}

// TestPairConsistencySameFuncBraceBearingParamFires is A3b: a parameter type
// with braces (`struct{ A int }`) must not open the body early. The opened-
// latch formulation (and go/parser) keys on the body block, not "any brace".
func TestPairConsistencySameFuncBraceBearingParamFires(t *testing.T) {
	src := `package p

func loadPageMetricsForCodesAndPages(
	ctx interface{},
	opts struct{ A int },
	tx interface{},
) error {
	_ = projectMetricUnionSQL // want
	tx.Query()
	return nil
}
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("brace-bearing param type must not swallow the body; got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "loadPageMetricsForCodesAndPages") {
		t.Fatalf("message must name the offending func, got %q", ms[0].Message)
	}
}

// TestPairConsistencySameFuncGreenwashSiblingDoesNotSatisfy is the hole
// same-file leaves open and the multi-line bug made unfixable: one Memo'd
// sibling must not greenwash a bare multi-line sibling in the same file.
func TestPairConsistencySameFuncGreenwashSiblingDoesNotSatisfy(t *testing.T) {
	src := `package p

func good(ctx, tx interface{}) error {
	return projectreadscope.Memo(ctx, "k", func() error {
		_ = projectMetricUnionSQL
		tx.Query()
		return nil
	})
}

func bad(
	ctx interface{},
	tx interface{},
) error {
	_ = projectMetricUnionSQL // bare sibling
	tx.Query()
	return nil
}
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("same-func must flag only the bare sibling, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "bad") {
		t.Fatalf("finding must name the bare func bad, got %q", ms[0].Message)
	}
	// same-file would report nothing (Memo present in file) — that is the
	// greenwash this mode exists to close.
}

// TestPairConsistencySameFuncMemoInSameBodyPasses: trigger and companion in
// one function body (including inside a nested func-literal Memo closure).
func TestPairConsistencySameFuncMemoInSameBodyPasses(t *testing.T) {
	src := `package p

func load(
	ctx interface{},
	tx interface{},
) error {
	return projectreadscope.Memo(ctx, "k", func() error {
		_ = projectMetricUnionSQL
		tx.Query()
		return nil
	})
}
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("metricload/load.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("memoized body must pass, got %+v", ms)
	}
}

// TestPairConsistencySameFuncNonGoFileYieldsNoFindings pins the goast-parity
// degradation: same-func has a unit vocabulary for .go, .dart and .proto
// only, so a file of any other extension in scope yields no findings and no
// error (same contract as internal/rules/goast parseGo). The spec documents
// this; a rule wanting text-grain pairing uses same-file.
func TestPairConsistencySameFuncNonGoFileYieldsNoFindings(t *testing.T) {
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("notes/query.md", []byte("projectMetricUnionSQL with no Memo\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("non-Go file must yield no findings, got %+v", ms)
	}
}

// TestPairConsistencySameFuncParseFailureIsError pins fail-closed: a .go file
// go/parser cannot parse is an engine error (exit 2), never a silent pass —
// a rule whose scope sweeps broken source must die loudly, not skip it.
func TestPairConsistencySameFuncParseFailureIsError(t *testing.T) {
	c := mustChecker(t, sameFuncParams)
	_, err := c.CheckFile(scan.NewMemFile("bad.go", []byte("package p\nfunc {{{\n")))
	if err == nil {
		t.Fatal("unparseable .go file must be an error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "bad.go") {
		t.Fatalf("parse error must name the file, got %v", err)
	}
}

// TestPairConsistencySameFuncNamesMethodReceiver pins funcDeclName's method
// path: a bare method must be reported as "(*T).Name" so free functions and
// methods are distinguishable in a mixed file.
func TestPairConsistencySameFuncNamesMethodReceiver(t *testing.T) {
	src := `package p

type Loader struct{}

func (l *Loader) LoadMetrics(tx interface{}) {
	_ = projectMetricUnionSQL
	tx.Query()
}
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("l.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, `(*Loader).LoadMetrics`) {
		t.Fatalf("method finding must name (*Loader).LoadMetrics, got %+v", ms)
	}
}

// TestPairConsistencySameFuncNamesGenericReceiver pins the IndexListExpr arm
// added in the lift: a multi-type-param generic receiver must name its base
// type, not fall to the anonymous "T" default.
func TestPairConsistencySameFuncNamesGenericReceiver(t *testing.T) {
	src := `package p

type Box[K comparable, V any] struct{}

func (b *Box[K, V]) LoadMetrics(tx interface{}) {
	_ = projectMetricUnionSQL
	tx.Query()
}
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("box.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || !strings.Contains(ms[0].Message, `(*Box).LoadMetrics`) {
		t.Fatalf("generic-receiver finding must name (*Box).LoadMetrics, got %+v", ms)
	}
}

// TestPairConsistencySameFuncFuncValuedVarOwesFinding closes the fail-open the
// upstream lift review found: `var loadX = func(...) {...}` — the standard
// test-seam refactor of `func loadX(...)` — is executable code no FuncDecl
// owns. Without this, a two-token stubbability refactor takes a function out
// of the lockdown silently. Each package-level func literal is its own unit,
// named by its var, so a Memo'd sibling var cannot greenwash a bare one; an
// IIFE initializer (`= func(...){...}(nil)`) is the same body one call deeper.
func TestPairConsistencySameFuncFuncValuedVarOwesFinding(t *testing.T) {
	src := `package p

var loadAnnotationMetrics = func(ctx interface{}, tx interface{}, projectID string) error {
	_, err := tx.Query(ctx, projectMetricUnionSQL, projectID)
	return err
}

var loadMemoized = func(ctx interface{}, tx interface{}, projectID string) error {
	return projectreadscope.Memo(ctx, "k", func() error {
		_, err := tx.Query(ctx, projectMetricUnionSQL, projectID)
		return err
	})
}

var metricsOnce = func(tx interface{}) error {
	_ = projectMetricUnionSQL
	tx.Query()
	return nil
}(nil)
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("seam.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("bare func-valued var and IIFE must each fire (memoized sibling must not), got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "loadAnnotationMetrics") {
		t.Fatalf("first finding must name the bare var loadAnnotationMetrics, got %q", ms[0].Message)
	}
	if !strings.Contains(ms[1].Message, "metricsOnce") {
		t.Fatalf("second finding must name the IIFE var metricsOnce, got %q", ms[1].Message)
	}
}

// TestPairConsistencySameFuncInitializerResidueStaysSilent pins a DELIBERATE
// non-behavior: package-level initializer code OUTSIDE any func literal (a
// definition site like `const projectMetricUnionSQL = ...`, or a direct
// init-time call) belongs to no unit and yields no finding. Treating bare
// initializer expressions as units would false-fire every rule whose trigger
// matches its own definition site — the const holding the SQL sits in the same
// scope the rule guards. The residue is disclosed in spec §5; a rule needing
// init-time coverage uses same-file.
func TestPairConsistencySameFuncInitializerResidueStaysSilent(t *testing.T) {
	src := `package p

const projectMetricUnionSQL = "SELECT ..."

var rows = mustQuery(projectMetricUnionSQL)

// The IIFE-ARGUMENT sub-shape: the trigger sits in the call's argument list,
// outside the literal's span. Closest neighbour to the var-bound unit above —
// pinned so a future "widen the unit to the whole IIFE CallExpr" change must
// confront this deliberately instead of flipping the residue silently.
var n = func(s string) int { return len(s) }(projectMetricUnionSQL)
`
	c := mustChecker(t, sameFuncParams)
	ms, err := c.CheckFile(scan.NewMemFile("def.go", []byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("initializer residue outside func literals must stay silent (disclosed), got %+v", ms)
	}
}

// TestPairConsistencySameFuncAlsoPresentGatesObligation restores the retired
// shell's has_query arm: a marker without tx.Query/QueryRow does not oblige Memo
// (Queue* siblings call the SQL builder without issuing a query).
func TestPairConsistencySameFuncAlsoPresentGatesObligation(t *testing.T) {
	params := sameFuncParams + "also_present: 'tx\\.(Query|QueryRow)\\('\n"
	c := mustChecker(t, params)

	// Marker only — Queue shape. No obligation.
	queue := `package p
func QueueProjectMetrics() {
	_ = projectMetricUnionSQL
}
`
	ms, err := c.CheckFile(scan.NewMemFile("q.go", []byte(queue)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Fatalf("marker without also_present match must not oblige Memo, got %+v", ms)
	}

	// Marker + Query, no Memo — obligation fires.
	bare := `package p
func ListProjectMetrics(tx interface{}) {
	_ = projectMetricUnionSQL
	tx.Query()
}
`
	ms, err = c.CheckFile(scan.NewMemFile("l.go", []byte(bare)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("marker+Query without Memo must fire, got %+v", ms)
	}
}

// TestPairConsistencySameFuncRejectsBadAlsoPresent pins compile-time rejection
// of a broken also_present regex (same shape as trigger/requires).
func TestPairConsistencySameFuncRejectsBadAlsoPresent(t *testing.T) {
	factory, _ := rules.Lookup("pair-consistency")
	_, err := factory(paramsNode(t, "trigger: t\nrequires: r\nwhere: same-func\nalso_present: '('\n"))
	if err == nil {
		t.Fatal("invalid also_present regex accepted")
	}
}

// TestPairConsistencyRejectsAlsoPresentOnlyForSameDir replaces an earlier test
// that pinned a blanket refusal outside same-func. #189 showed that constraint
// was too wide: also_present gates on a unit's text span, and same-file's unit
// (the file, and the default when `where` is unset) holds one. Only same-dir
// does not — its unit is a directory assembled across files in Finalize.
//
// The narrowing is kept here rather than deleted, because the remaining refusal
// is the load-bearing half: a gate on a unit with no span must refuse at config
// time, not resolve to "not owed". Acceptance for same-file is pinned in
// alsopresent_samefile_test.go, in both directions.
func TestPairConsistencyRejectsAlsoPresentOnlyForSameDir(t *testing.T) {
	factory, _ := rules.Lookup("pair-consistency")

	_, err := factory(paramsNode(t, "trigger: t\nrequires: r\nwhere: same-dir\nalso_present: q\n"))
	if err == nil {
		t.Fatal("also_present accepted with where: same-dir")
	}
	if !strings.Contains(err.Error(), "same-dir") {
		t.Fatalf("rejection must name the mode that cannot serve also_present, got %v", err)
	}

	for _, params := range []string{
		"trigger: t\nrequires: r\nwhere: same-file\nalso_present: q\n",
		"trigger: t\nrequires: r\nalso_present: q\n", // where unset -> same-file
		"trigger: t\nrequires: r\nwhere: same-func\nalso_present: q\n",
	} {
		if _, err := factory(paramsNode(t, params)); err != nil {
			t.Fatalf("also_present must be accepted where the unit holds a span: %q: %v", params, err)
		}
	}
}
