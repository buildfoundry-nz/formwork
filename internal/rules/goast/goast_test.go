package goast_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

func build(t *testing.T, typ, params string) rules.Checker {
	t.Helper()
	f, ok := rules.Lookup(typ)
	if !ok {
		t.Fatalf("%s type not registered", typ)
	}
	var node *yaml.Node
	if params != "" {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Content) > 0 {
			node = doc.Content[0]
		}
	}
	c, err := f(node)
	if err != nil {
		t.Fatalf("build %s %q: %v", typ, params, err)
	}
	return c
}

func buildErr(t *testing.T, typ, params string) error {
	t.Helper()
	f, ok := rules.Lookup(typ)
	if !ok {
		t.Fatalf("%s type not registered", typ)
	}
	var node *yaml.Node
	if params != "" {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(params), &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Content) > 0 {
			node = doc.Content[0]
		}
	}
	_, err := f(node)
	return err
}

func check(t *testing.T, c rules.Checker, path, src string) []rules.Match {
	t.Helper()
	ms, err := c.CheckFile(scan.NewMemFile(path, []byte(src)))
	if err != nil {
		t.Fatalf("CheckFile(%s): %v", path, err)
	}
	return ms
}

// --- shared behavior: non-go skip and parse errors ---

func TestNonGoFileSkipped(t *testing.T) {
	for _, typ := range []string{
		"go/func-line-budget",
		"go/call-confined-to-func-name",
		"go/call-order-in-func",
		"go/guard-precedes-call",
		"go/per-func-count-relation",
	} {
		c := buildDefault(t, typ)
		ms, err := c.CheckFile(scan.NewMemFile("notes.txt", []byte("func main() { not go }\n")))
		if err != nil {
			t.Fatalf("%s: non-go file errored: %v", typ, err)
		}
		if len(ms) != 0 {
			t.Fatalf("%s: non-go file flagged: %+v", typ, ms)
		}
	}
}

func TestUnparseableGoFileErrors(t *testing.T) {
	for _, typ := range []string{
		"go/func-line-budget",
		"go/call-confined-to-func-name",
		"go/call-order-in-func",
		"go/guard-precedes-call",
		"go/per-func-count-relation",
	} {
		c := buildDefault(t, typ)
		_, err := c.CheckFile(scan.NewMemFile("broken.go", []byte("package p\nfunc (\n")))
		if err == nil {
			t.Fatalf("%s: unparseable .go returned no error", typ)
		}
	}
}

func buildDefault(t *testing.T, typ string) rules.Checker {
	t.Helper()
	switch typ {
	case "go/func-line-budget":
		return build(t, typ, "max_lines: 3\n")
	case "go/call-confined-to-func-name":
		return build(t, typ, "symbol: 'os\\.Exit'\nallowed_func: 'main'\n")
	case "go/call-order-in-func":
		return build(t, typ, "funcs: '.'\nsequence: ['setup', 'run']\n")
	case "go/guard-precedes-call":
		return build(t, typ, "guard: 'lock'\nsink: 'write'\n")
	case "go/per-func-count-relation":
		return build(t, typ, "left: 'acquire'\nright: 'release'\nrelation: '=='\n")
	}
	t.Fatalf("no default for %s", typ)
	return nil
}

// --- go/func-line-budget ---

func TestFuncLineBudgetPass(t *testing.T) {
	c := build(t, "go/func-line-budget", "max_lines: 3\n")
	src := "package p\nfunc small() {\n\tx := 1\n\t_ = x\n}\n"
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("small func flagged: %+v", ms)
	}
}

func TestFuncLineBudgetFail(t *testing.T) {
	c := build(t, "go/func-line-budget", "max_lines: 3\n")
	src := "package p\nfunc big() {\n\ta()\n\tb()\n\tc()\n\td()\n\te()\n}\n"
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("want 1 finding, got %+v", ms)
	}
	if ms[0].Line != 2 {
		t.Fatalf("want finding at func decl line 2, got %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "big") {
		t.Fatalf("message should name func: %q", ms[0].Message)
	}
}

func TestFuncLineBudgetFuncsFilter(t *testing.T) {
	// Only funcs matching 'keep' are budgeted; big 'skip' func is ignored.
	c := build(t, "go/func-line-budget", "max_lines: 1\nfuncs: 'keep'\n")
	src := "package p\nfunc skipMe() {\n\ta()\n\tb()\n\tc()\n}\nfunc keepMe() {\n\tx()\n\ty()\n\tz()\n}\n"
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("want only keepMe flagged, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "keepMe") {
		t.Fatalf("wrong func flagged: %q", ms[0].Message)
	}
}

func TestFuncLineBudgetRequiresMaxLines(t *testing.T) {
	if err := buildErr(t, "go/func-line-budget", "funcs: 'x'\n"); err == nil {
		t.Fatal("missing max_lines accepted")
	}
}

func TestFuncLineBudgetRejectsBadFuncsRegex(t *testing.T) {
	if err := buildErr(t, "go/func-line-budget", "max_lines: 2\nfuncs: '('\n"); err == nil {
		t.Fatal("bad funcs regex accepted")
	}
}

// --- go/call-confined-to-func-name ---

func TestCallConfinedPass(t *testing.T) {
	c := build(t, "go/call-confined-to-func-name", "symbol: 'os\\.Exit'\nallowed_func: 'main'\n")
	src := "package main\nfunc main() {\n\tos.Exit(1)\n}\n"
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("confined call flagged: %+v", ms)
	}
}

func TestCallConfinedFail(t *testing.T) {
	c := build(t, "go/call-confined-to-func-name", "symbol: 'os\\.Exit'\nallowed_func: 'main'\n")
	src := "package main\nfunc helper() {\n\tos.Exit(1)\n}\nfunc main() {}\n"
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("want 1 finding, got %+v", ms)
	}
	if ms[0].Line != 3 {
		t.Fatalf("want finding at call line 3, got %d", ms[0].Line)
	}
	if !strings.Contains(ms[0].Message, "helper") {
		t.Fatalf("message should name offending func: %q", ms[0].Message)
	}
}

func TestCallConfinedRequiresParams(t *testing.T) {
	if err := buildErr(t, "go/call-confined-to-func-name", "symbol: 'x'\n"); err == nil {
		t.Fatal("missing allowed_func accepted")
	}
	if err := buildErr(t, "go/call-confined-to-func-name", "allowed_func: 'x'\n"); err == nil {
		t.Fatal("missing symbol accepted")
	}
}

// --- go/call-order-in-func ---

func TestCallOrderPass(t *testing.T) {
	c := build(t, "go/call-order-in-func", "funcs: '.'\nsequence: ['setup', 'run', 'teardown']\n")
	src := "package p\nfunc f() {\n\tsetup()\n\trun()\n\tteardown()\n}\n"
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("ordered calls flagged: %+v", ms)
	}
}

func TestCallOrderFail(t *testing.T) {
	c := build(t, "go/call-order-in-func", "funcs: 'f'\nsequence: ['setup', 'run']\n")
	src := "package p\nfunc f() {\n\trun()\n\tsetup()\n}\n"
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("want 1 finding, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "setup") || !strings.Contains(ms[0].Message, "run") {
		t.Fatalf("message should name the out-of-order pair: %q", ms[0].Message)
	}
}

func TestCallOrderRequiresTwoSeq(t *testing.T) {
	if err := buildErr(t, "go/call-order-in-func", "funcs: 'f'\nsequence: ['only']\n"); err == nil {
		t.Fatal("single-element sequence accepted")
	}
	if err := buildErr(t, "go/call-order-in-func", "funcs: 'f'\n"); err == nil {
		t.Fatal("missing sequence accepted")
	}
	if err := buildErr(t, "go/call-order-in-func", "sequence: ['a','b']\n"); err == nil {
		t.Fatal("missing funcs accepted")
	}
}

// --- go/guard-precedes-call ---

func TestGuardPrecedesPass(t *testing.T) {
	c := build(t, "go/guard-precedes-call", "guard: 'lock'\nsink: 'write'\n")
	src := "package p\nfunc h() {\n\tlock()\n\twrite()\n\twrite()\n}\n"
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("guarded sinks flagged: %+v", ms)
	}
}

func TestGuardPrecedesFail(t *testing.T) {
	c := build(t, "go/guard-precedes-call", "guard: 'lock'\nsink: 'write'\n")
	src := "package p\nfunc h() {\n\twrite()\n\tlock()\n\twrite()\n}\n"
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("want 1 finding for the unguarded sink, got %+v", ms)
	}
	if ms[0].Line != 3 {
		t.Fatalf("want finding at unguarded sink line 3, got %d", ms[0].Line)
	}
}

func TestGuardRequiresParams(t *testing.T) {
	if err := buildErr(t, "go/guard-precedes-call", "guard: 'x'\n"); err == nil {
		t.Fatal("missing sink accepted")
	}
	if err := buildErr(t, "go/guard-precedes-call", "sink: 'x'\n"); err == nil {
		t.Fatal("missing guard accepted")
	}
}

// --- go/per-func-count-relation ---

func TestCountRelationPass(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "left: 'acquire'\nright: 'release'\nrelation: '=='\n")
	src := "package p\nfunc g() {\n\tacquire()\n\trelease()\n}\n"
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("balanced func flagged: %+v", ms)
	}
}

func TestCountRelationFail(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "left: 'acquire'\nright: 'release'\nrelation: '=='\n")
	src := "package p\nfunc g() {\n\tacquire()\n\tacquire()\n\trelease()\n}\n"
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("want 1 finding, got %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "g") {
		t.Fatalf("message should name func: %q", ms[0].Message)
	}
}

func TestCountRelationGTE(t *testing.T) {
	c := build(t, "go/per-func-count-relation", "left: 'acquire'\nright: 'release'\nrelation: '>='\n")
	// 1 acquire >= 2 release is false -> violation.
	src := "package p\nfunc g() {\n\tacquire()\n\trelease()\n\trelease()\n}\n"
	if ms := check(t, c, "a.go", src); len(ms) != 1 {
		t.Fatalf("want 1 finding for >= violation, got %+v", ms)
	}
}

func TestCountRelationRejectsBadRelation(t *testing.T) {
	if err := buildErr(t, "go/per-func-count-relation", "left: 'a'\nright: 'b'\nrelation: '!='\n"); err == nil {
		t.Fatal("bad relation accepted")
	}
	if err := buildErr(t, "go/per-func-count-relation", "left: 'a'\nright: 'b'\n"); err == nil {
		t.Fatal("missing relation accepted")
	}
}

// --- import-alias / dot-import / func-value / require_used ---
//
// The count-relation used to match the literal Fun spelling. An import alias
// (qs.Redeem), a dot-import (bare Redeem), a func-value alias
// (fn := quoteshare.Redeem; fn()), or a discarded gate call all kept a
// security rule green. These tests pin each shape closed.

func TestCountRelationResolvesImportAlias(t *testing.T) {
	c := build(t, "go/per-func-count-relation",
		"left: '^quoteshare\\.Redeem$'\nright: '^clientportal\\.RequireEnabled$'\nrelation: '<='\n")
	src := `package p
import (
	qs "example.com/internal/quoteshare"
)
func DownloadArtifact() {
	qs.Redeem()
}
`
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("alias Redeem without gate must fire, got %+v", ms)
	}
}

func TestCountRelationResolvesDotImport(t *testing.T) {
	c := build(t, "go/per-func-count-relation",
		"left: '^quoteshare\\.Redeem$'\nright: '^clientportal\\.RequireEnabled$'\nrelation: '<='\n")
	src := `package p
import . "example.com/internal/quoteshare"
func DownloadArtifact() {
	Redeem()
}
`
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("dot-import bare Redeem without gate must fire, got %+v", ms)
	}
}

func TestCountRelationResolvesFuncValue(t *testing.T) {
	c := build(t, "go/per-func-count-relation",
		"left: '^quoteshare\\.Redeem$'\nright: '^clientportal\\.RequireEnabled$'\nrelation: '<='\n")
	src := `package p
import "example.com/internal/quoteshare"
func DownloadArtifact() {
	redeemFn := quoteshare.Redeem
	redeemFn()
}
`
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("func-value Redeem without gate must fire, got %+v", ms)
	}
}

func TestCountRelationRequireUsedIgnoresDiscardedRight(t *testing.T) {
	c := build(t, "go/per-func-count-relation",
		"left: '^quoteshare\\.Redeem$'\nright: '^clientportal\\.RequireEnabled$'\nrelation: '<='\nrequire_used: right\n")
	// Redeem used; RequireEnabled as bare statement — results discarded.
	// Without require_used this would be 1 <= 1 (green); with it, 1 <= 0.
	src := `package p
import (
	"example.com/internal/clientportal"
	"example.com/internal/quoteshare"
)
func DownloadArtifact() {
	_ = quoteshare.Redeem()
	clientportal.RequireEnabled()
}
`
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("discarded RequireEnabled must not count as right, got %+v", ms)
	}
}

func TestCountRelationRequireUsedAcceptsCheckedRight(t *testing.T) {
	c := build(t, "go/per-func-count-relation",
		"left: '^quoteshare\\.Redeem$'\nright: '^clientportal\\.RequireEnabled$'\nrelation: '<='\nrequire_used: right\n")
	src := `package p
import (
	"example.com/internal/clientportal"
	"example.com/internal/quoteshare"
)
func DownloadArtifact() {
	_, _ = quoteshare.Redeem()
	if err := clientportal.RequireEnabled(); err != nil {
		return
	}
}
`
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("checked RequireEnabled must count as right, got %+v", ms)
	}
}

func TestCountRelationAliasPassWithGate(t *testing.T) {
	c := build(t, "go/per-func-count-relation",
		"left: '^quoteshare\\.Redeem$'\nright: '^clientportal\\.RequireEnabled$'\nrelation: '<='\nrequire_used: right\n")
	src := `package p
import (
	cp "example.com/internal/clientportal"
	qs "example.com/internal/quoteshare"
)
func DownloadArtifact() {
	_, err := qs.Redeem()
	_ = err
	if err := cp.RequireEnabled(); err != nil {
		return
	}
}
`
	if ms := check(t, c, "a.go", src); len(ms) != 0 {
		t.Fatalf("aliased Redeem+RequireEnabled must pass, got %+v", ms)
	}
}

func TestCountRelationRejectsBadRequireUsed(t *testing.T) {
	if err := buildErr(t, "go/per-func-count-relation",
		"left: 'a'\nright: 'b'\nrelation: '=='\nrequire_used: middle\n"); err == nil {
		t.Fatal("bad require_used accepted")
	}
}

func TestCallConfinedResolvesImportAlias(t *testing.T) {
	c := build(t, "go/call-confined-to-func-name",
		"symbol: '^quoteshare\\.Redeem$'\nallowed_func: '^(View|Accept|Decline|DownloadArtifact)$'\n")
	src := `package p
import qs "example.com/internal/quoteshare"
func helper() {
	qs.Redeem()
}
`
	ms := check(t, c, "a.go", src)
	if len(ms) != 1 {
		t.Fatalf("aliased Redeem outside allowed funcs must fire, got %+v", ms)
	}
}

func TestRejectsUnknownField(t *testing.T) {
	if err := buildErr(t, "go/func-line-budget", "max_lines: 2\nnope: 1\n"); err == nil {
		t.Fatal("unknown field accepted")
	}
}
