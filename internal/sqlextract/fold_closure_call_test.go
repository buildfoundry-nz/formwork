// fold_closure_call_test.go — #337, the invocation half of #72.
//
// #72 keyed the named-closure untrack on the closure being INVOKED. That key is
// right: a closure that is never called, called conditionally, or created after
// the value is used leaves the outside-appends world REAL, and untracking those
// deletes true positives (spec §10 measured it: ten findings gone, eight of them
// true). What was wrong is how narrowly "invoked" was read — one spelling, an
// *ast.ExprStmt in body.List whose Fun is a bare *ast.Ident. Write the identical
// unconditional call any other way and the append was dropped while the variable
// stayed tracked, so the fold emitted a value no execution path produces.
//
// The table below is the spellings where the call PROVABLY RUNS and the pass can
// see it, so untracking costs no true positive whatsoever. Each is asserted in
// both directions, because only one of them is visible at the gate:
//
//   - the closure adds the ORDER BY: the fold's world is an unordered locking
//     SELECT the code never produces, and the rule FIRES on safe code.
//   - the closure adds the LOCK: the fold's world drops the lock, so the rule
//     goes SILENT on a real deadlock hazard. Nothing downstream can see this
//     one — a missing candidate and a candidate that passes both read as zero
//     findings — so it is pinned here or nowhere.
//
// nocall is the control for each shape, and it is the same source with the call
// spelled as a non-call. Without it every assertion here would be satisfied by a
// fix that untracked the query unconditionally, which is the rejected design.
package sqlextract_test

import (
	"strings"
	"testing"
)

// callShape is one spelling of "this closure ran", with @IN standing for the
// text the closure appends and @OUT for the text appended after the call.
type callShape struct {
	name   string
	body   string
	nocall string
}

func shapeBody(tmpl, in, out string) string {
	return strings.NewReplacer("@IN", in, "@OUT", out).Replace(tmpl)
}

// closureFolds reassembles a block and keeps the fold worlds built on the seed.
//
// The harness declares BOTH a helper that calls its argument and one that does
// not, in one file, because that pair is the whole argument of
// TestClosureNameHandedToAHelperKeepsFolding below: they are indistinguishable
// to a parse-only pass.
func closureFolds(t *testing.T, body string) []string {
	t.Helper()
	src := "package db\n\nfunc exec(s string) {}\n" +
		"func run(f func()) { f() }\nfunc register(f func()) {}\n\n" +
		"type hooks struct{ order func() }\n\ntype rows struct{}\n\nfunc (rows) order() {}\n\n" +
		"func load(b bool, xs []int, ch chan int, m map[string]int) string {\n" + body + "\treturn q\n}\n"
	return foldOnly(foldTexts(t, src), iifeSeed)
}

const closureSeedLine = "\tq := `SELECT id FROM t WHERE s = 'x'`\n"
const closureBind = "\tadd := func() { q += \"@IN\" }\n"
const closureTail = "\tq += \"@OUT\"\n"

// closureStrBind is the same closure with a result, for the spellings whose call
// sits in an expression rather than a statement of its own.
const closureStrBind = "\tadd := func() string { q += \"@IN\"; return \"\" }\n"

// Every spelling here runs the closure on every path through the block, and
// every one of them is visible to a parse-only pass — no call boundary is
// crossed and no branch is guessed at.
var provablyRunCalls = []callShape{
	{
		name:   "a bare block",
		body:   closureSeedLine + closureBind + "\t{\n\t\tadd()\n\t}\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\t{\n\t\t_ = add\n\t}\n" + closureTail,
	},
	{
		name:   "a bare block two deep",
		body:   closureSeedLine + closureBind + "\t{\n\t\t{\n\t\t\tadd()\n\t\t}\n\t}\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\t{\n\t\t{\n\t\t\t_ = add\n\t\t}\n\t}\n" + closureTail,
	},
	{
		name:   "an alias",
		body:   closureSeedLine + closureBind + "\tg := add\n\tg()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\tg := add\n\t_ = g\n" + closureTail,
	},
	{
		name:   "an alias chain",
		body:   closureSeedLine + closureBind + "\tg := add\n\th := g\n\th()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\tg := add\n\th := g\n\t_ = h\n" + closureTail,
	},
	{
		name:   "an alias declared with var",
		body:   closureSeedLine + closureBind + "\tvar g = add\n\tg()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\tvar g = add\n\t_ = g\n" + closureTail,
	},
	{
		name:   "an immediately-invoked literal",
		body:   closureSeedLine + closureBind + "\tfunc() { add() }()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\tfunc() { _ = add }()\n" + closureTail,
	},
	{
		name:   "an immediately-invoked literal two deep",
		body:   closureSeedLine + closureBind + "\tfunc() { func() { add() }() }()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\tfunc() { func() { _ = add }() }()\n" + closureTail,
	},
	{
		name:   "a closure bound with var",
		body:   closureSeedLine + "\tvar add = func() { q += \"@IN\" }\n\tadd()\n" + closureTail,
		nocall: closureSeedLine + "\tvar add = func() { q += \"@IN\" }\n\t_ = add\n" + closureTail,
	},
	{
		name:   "a call in an assignment value",
		body:   closureSeedLine + closureStrBind + "\ts := add()\n\t_ = s\n" + closureTail,
		nocall: closureSeedLine + closureStrBind + "\ts := \"\"\n\t_ = s\n" + closureTail,
	},
	{
		name:   "a call in an if init",
		body:   closureSeedLine + closureStrBind + "\tif s := add(); s != \"\" {\n\t\texec(s)\n\t}\n" + closureTail,
		nocall: closureSeedLine + closureStrBind + "\tif s := \"\"; s != \"\" {\n\t\texec(s)\n\t}\n" + closureTail,
	},
	{
		name:   "a call in an if condition",
		body:   closureSeedLine + closureStrBind + "\tif add() != \"\" {\n\t\texec(\"\")\n\t}\n" + closureTail,
		nocall: closureSeedLine + closureStrBind + "\tif b {\n\t\texec(\"\")\n\t}\n" + closureTail,
	},
	{
		name:   "a call in a var initialiser",
		body:   closureSeedLine + closureStrBind + "\tvar s = add()\n\t_ = s\n" + closureTail,
		nocall: closureSeedLine + closureStrBind + "\tvar s = \"\"\n\t_ = s\n" + closureTail,
	},
	{
		name:   "a call in a channel send",
		body:   closureSeedLine + "\tadd := func() int { q += \"@IN\"; return 0 }\n\tch <- add()\n" + closureTail,
		nocall: closureSeedLine + "\tadd := func() int { q += \"@IN\"; return 0 }\n\tch <- 0\n" + closureTail,
	},
	{
		name:   "a call in an assignment target",
		body:   closureSeedLine + closureStrBind + "\tm[add()] = 1\n" + closureTail,
		nocall: closureSeedLine + closureStrBind + "\tm[\"\"] = 1\n" + closureTail,
	},
	{
		name:   "a labelled call",
		body:   closureSeedLine + closureBind + "\tif b {\n\t\tgoto call\n\t}\ncall:\n\tadd()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\tif b {\n\t\tgoto call\n\t}\ncall:\n\t_ = add\n" + closureTail,
	},
	{
		name:   "a parenthesised call",
		body:   closureSeedLine + closureBind + "\t(add)()\n" + closureTail,
		nocall: closureSeedLine + closureBind + "\t_ = (add)\n" + closureTail,
	},
	{
		name: "a self-recursive literal",
		body: closureSeedLine + "\tvar add func()\n\tadd = func() { q += \"@IN\"; add() }\n" +
			"\tadd()\n" + closureTail,
		nocall: closureSeedLine + "\tvar add func()\n\tadd = func() { q += \"@IN\"; add() }\n" +
			"\t_ = add\n" + closureTail,
	},
	{
		name: "a closure nested two levels",
		body: closureSeedLine + "\touter := func() { inner := func() { q += \"@IN\" }; inner() }\n" +
			"\touter()\n" + closureTail,
		nocall: closureSeedLine + "\touter := func() { inner := func() { q += \"@IN\" }; inner() }\n" +
			"\t_ = outer\n" + closureTail,
	},
}

// The noisy direction. The closure supplies the ORDER BY on every path, so the
// real value is a safe ordered locking SELECT; the world assembled from the
// outside appends alone is an unordered locking SELECT no path produces, and
// the gate reports a deadlock hazard the code does not have.
func TestProvablyCalledClosureDoesNotFabricateTheUnorderedWorld(t *testing.T) {
	for _, tc := range provablyRunCalls {
		t.Run(tc.name, func(t *testing.T) {
			folds := closureFolds(t, shapeBody(tc.body, " ORDER BY id", " FOR UPDATE"))
			if hasFoldText(folds, iifeSeed+" FOR UPDATE") {
				t.Fatalf("the closure runs on every path, so the world that drops its "+
					"ORDER BY exists on no path: %q", folds)
			}
		})
	}
}

// The silent direction, and the one this rule exists for. The closure supplies
// the LOCK, so the real value is an unordered locking SELECT — a genuine
// deadlock hazard. The world assembled from the outside appends alone is an
// ordered non-locking query, which reads as safe and silences it.
func TestProvablyCalledClosureDoesNotFabricateTheOrderedWorld(t *testing.T) {
	for _, tc := range provablyRunCalls {
		t.Run(tc.name, func(t *testing.T) {
			folds := closureFolds(t, shapeBody(tc.body, " FOR UPDATE", " ORDER BY id"))
			if hasFoldText(folds, iifeSeed+" ORDER BY id") {
				t.Fatalf("the closure runs on every path and adds the lock, so the "+
					"world that drops it exists on no path: %q", folds)
			}
		})
	}
}

// The control, one per shape: the same source with the call spelled as a
// non-call. Every assertion above would be satisfied by a fix that untracked
// the query outright — the design spec §10 records as rejected for deleting
// eight true positives — and this is what fails if one does.
func TestUncalledClosureInTheSameShapeStillFolds(t *testing.T) {
	for _, tc := range provablyRunCalls {
		t.Run(tc.name, func(t *testing.T) {
			folds := closureFolds(t, shapeBody(tc.nocall, " ORDER BY id", " FOR UPDATE"))
			if !hasFoldText(folds, iifeSeed+" FOR UPDATE") {
				t.Fatalf("the closure never runs, so the outside-appends world is the "+
					"real one and must be emitted: %q", folds)
			}
		})
	}
}

// THE DECISION THIS BATCH MAKES AND DOES NOT HIDE: `run(add)` — the headline of
// #337 — keeps folding, and the false positive it produces stays.
//
// The closure's NAME escapes into a call, unconditionally. Noticing the escape
// is trivial; ACTING on it is what costs. Whether the callee invokes f or merely
// stores it is cross-function flow, which spec §2 "Out" declines outright — "the
// extractor is deliberately parse-only so it works on a tree that does not
// compile" — so `func run(f func()) { f() }` and `func register(f func()) {}`
// are one program to this pass. The harness declares both, in the same file, to
// make that concrete rather than asserted.
//
// Untracking on the escape therefore does two things, both measured on this
// tree. It silences `register(add)`, where the closure never runs and
// `… FOR UPDATE` IS the real value — the genuinely unordered locking SELECT
// this rule exists to catch. And it splits two spellings of one program:
// fold_iife_test.go:58's LOAD-BEARING GUARD pins the anonymous spelling,
// `run(func(){ q += " ORDER BY id" })`, green, and its comment says re-making
// that deletion is spec §10's rejected design. `run(add)` untracked while
// `run(func(){…})` folds is incoherent, and the deletion is the larger loss.
//
// So the residue is real, and what closes #337 is that it stops being
// undisclosed and untested. This test is the pin.
func TestClosureNameHandedToAHelperKeepsFolding(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"a helper that calls it", closureSeedLine + closureBind + "\trun(add)\n" + closureTail},
		{"a helper that does not", closureSeedLine + closureBind + "\tregister(add)\n" + closureTail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folds := closureFolds(t, shapeBody(tc.body, " ORDER BY id", " FOR UPDATE"))
			if !hasFoldText(folds, iifeSeed+" FOR UPDATE") {
				t.Fatalf("a parse-only pass cannot tell a helper that calls f from one "+
					"that stores it; untracking here silences the register() hazard and "+
					"splits this from the anonymous spelling fold_iife_test.go pins: %q", folds)
			}
		})
	}
}

// The other narrowing, and it is what keeps the widened invocation walk from
// becoming the rejected refusal design. In every shape below the closure is NOT
// proven to run where the value is used, so the world assembled from the outside
// appends alone is a REAL execution path and emitting it is correct — a finding
// on it is a true positive, the unordered locking SELECT the rule exists for.
//
// They fall into three groups, and the reason differs by group:
//
//   - SCHEDULED, not run: `defer add()` fires after `return q` has evaluated,
//     and `go add()` races the use. The outside-only value is what the caller
//     sees.
//   - CONDITIONAL: a loop body runs zero times on an empty slice, a switch case
//     may match nothing, a select clause is one of several. Same reason
//     escape.go's scanRun lists these as the statements it does not walk.
//   - NOT PROVABLY THIS CLOSURE: the callee is a field, an element, or a method
//     value, so no name this pass resolves says which function runs. Storing a
//     closure is not evidence of calling it — `hooks{order: add}` with nothing
//     ever calling h.order is the register(add) case again, and untracking on
//     the store deletes that hazard.
func TestClosureNotProvablyCalledStillFolds(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"deferred", closureSeedLine + closureBind + "\tdefer add()\n" + closureTail},
		{"started in a goroutine", closureSeedLine + closureBind + "\tgo add()\n" + closureTail},
		{"in a range body", closureSeedLine + closureBind + "\tfor range xs {\n\t\tadd()\n\t}\n" + closureTail},
		{"in a switch case", closureSeedLine + closureBind + "\tswitch {\n\tcase b:\n\t\tadd()\n\t}\n" + closureTail},
		{"in a select clause", closureSeedLine + closureBind + "\tselect {\n\tcase <-ch:\n\t\tadd()\n\t}\n" + closureTail},
		{"through a struct field", closureSeedLine + closureBind + "\th := hooks{order: add}\n\th.order()\n" + closureTail},
		{"through a slice element", closureSeedLine + closureBind + "\tfs := []func(){add}\n\tfs[0]()\n" + closureTail},
		{"a method value", closureSeedLine + closureBind + "\tvar r rows\n\tmv := r.order\n\tmv()\n\t_ = add\n" + closureTail},
		{"a self-recursive literal never called", closureSeedLine +
			"\tvar add func()\n\tadd = func() { q += \"@IN\"; add() }\n\t_ = add\n" + closureTail},
		{"a nested closure never called", closureSeedLine +
			"\touter := func() { inner := func() { q += \"@IN\" }; inner() }\n\t_ = outer\n" + closureTail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folds := closureFolds(t, shapeBody(tc.body, " ORDER BY id", " FOR UPDATE"))
			if !hasFoldText(folds, iifeSeed+" FOR UPDATE") {
				t.Fatalf("the closure is not proven to run before the value is used, so "+
					"the outside-appends world is a real path and must be emitted: %q", folds)
			}
		})
	}
}
