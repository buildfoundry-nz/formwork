package sqlextract

import (
	"go/ast"
	"go/token"
)

// invoked.go — #337, the other half of #72: WHICH SPELLINGS OF "THIS CLOSURE
// RAN" THE PASS RECOGNISES.
//
// #72 keyed the named-closure untrack on the closure being INVOKED, and that
// key is right. A closure that is never called, called conditionally, or created
// after the value is used leaves the world assembled from the appends OUTSIDE it
// REAL — the not-called path — so emitting it is correct and a finding on it is
// a true positive. Spec §10 records the design that untracked them anyway: it
// removed ten findings, eight of them true.
//
// What was wrong is how narrowly "invoked" was read: one spelling, an
// *ast.ExprStmt in body.List whose Fun is a bare *ast.Ident. Written any other
// way the same unconditional call was invisible, so the closure's append was
// dropped WHILE THE VARIABLE STAYED TRACKED and the fold emitted a value no
// execution path produces (spec §4.1 promises never to emit one) — the #72/#73/
// #74 wrong-emit class on shapes none of them named. The binding side read one
// spelling too: only an *ast.AssignStmt, so `var add = func(){ … }` was not a
// closure at all.
//
// THE QUESTION IS THE ONE escape.go ALREADY ASKS — which statements does this
// block provably run — and it is asked here in the same shape, deliberately, so
// that the next syntax form nobody thought of is covered by construction rather
// than by another case. What is NOT walked is the point of the walk, so it is
// named: a `for`/`range` body runs zero times on an empty slice, a `switch` case
// may match nothing, a `select` clause is one of several, an `if` arm is one of
// two, `defer` and `go` schedule a call that has not run where the value is
// used, and a `return` expression evaluates after every append. In every one of
// those the outside-appends world is a real path.
//
// ONE DIFFERENCE FROM scanRun, and it is not an oversight: this walk DOES enter
// a bare nested block. scanRun refuses to, because the name it collects is read
// in the nested scope — a `q := …` there shadows the outer q, and untracking the
// outer one on an inner one's escape would delete a true positive. Here the name
// collected at the call site is the CLOSURE's, and the variables it untracks are
// read at the closure's BINDING site, not at the call. So entering a block adds
// no scope-blindness the binding side does not already have: closureAppends
// inspects the whole body, at every depth, exactly as #72 shipped it. The
// residue that leaves is one shape, measured rather than reasoned about, and it
// is named on the block arm of scanInvoked.
//
// A NAME HANDED TO A CALL IS NOT A CALL. `run(add)` — #337's headline — is
// deliberately not an invocation here: whether the callee invokes f or merely
// stores it is cross-function flow, which spec §2 "Out" declines outright ("the
// extractor is deliberately parse-only so it works on a tree that does not
// compile"), so `func run(f func()){ f() }` and `func register(f func()){}` are
// one program to this pass. Untracking on the escape silences the register()
// case, where the closure never runs and the unordered locking SELECT is the
// REAL value, and it splits `run(add)` from `run(func(){ … })`, which
// fold_iife_test.go pins green as spec §10's rejected design.
//
// NOTICED, THOUGH, AND CARRIED, which is the other reading of #337's own
// premise. The issue says detecting the escape "needs no interprocedural
// analysis, only noticing that the name escapes", and that is right: noticing
// is free and acting is what costs a true positive. escapedClosureRisk below
// spends the notice on the operator instead — it records the escape and the
// spelling of it, untracks nothing, and adds or removes no candidate. What this
// false positive costs an adopter is not being able to tell it from a real one,
// and that is a disclosure problem rather than an analysis one.
//
// The false positive therefore stands, and it stands ON THE RECORD:
// TestClosureNameHandedToAHelperKeepsFolding pins it at the fold and
// TestLockingClosureNameHandedToAHelperFires at the gate, each carrying the
// reasoning above, so the next reader who thinks this is a missed spelling finds
// the measurement instead of repeating it.

// closureAppends maps a name bound to a func literal in body to the set of
// variables that literal assigns — with an alias resolved to what it names, so
// `g := add; g()` is the same fact as `add()`.
//
// Two spellings of a binding are read, because they say the same thing about the
// name: an assignment (`add := func(){ … }`, `add = func(){ … }`) and a `var`
// value spec (`var add = func(){ … }`). Only the positional form is paired —
// len(Lhs) == len(Rhs) — which is exactly Go's own rule; the unbalanced form
// (`a, b := f()`) unpacks a call and yields no literal to read.
//
// SCOPE-BLIND, at two points, both inherited from #72 and both stated rather
// than implied. The body is inspected at every depth, so a literal bound inside
// a nested block or inside another literal is collected under its bare name; and
// the variables it appends to are read the same way, so a `q += …` writing a
// name the literal itself BOUND is recorded against the outer q. Deciding either
// properly needs the scope stack this pass does not have, and both are how #72
// shipped.
//
// NOT CORRELATED WITH ORDER either. A name rebound to a second literal keeps the
// last APPENDING one, and nothing here relates a rebinding to the call site: for
// `add := L1; add(); add = L2` the call really did run L1 and it is read that
// way, while for `add := L1; add = L2; add()` L1 never runs and its appends
// untrack anyway, costing a candidate. An alias contributes the union of what
// its sources name, for the same reason.
func closureAppends(body *ast.BlockStmt) map[string]map[string]token.Pos {
	appends := map[string]map[string]token.Pos{}
	alias := map[string][]string{}
	bind := func(lhs, rhs ast.Expr) {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok {
			return
		}
		switch v := ast.Unparen(rhs).(type) {
		case *ast.FuncLit:
			if touched := literalAppends(v); len(touched) > 0 {
				appends[id.Name] = touched
			}
		case *ast.Ident:
			alias[id.Name] = append(alias[id.Name], v.Name)
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) == len(x.Rhs) {
				for i := range x.Lhs {
					bind(x.Lhs[i], x.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if len(x.Names) == len(x.Values) {
				for i := range x.Names {
					bind(x.Names[i], x.Values[i])
				}
			}
		}
		return true
	})
	resolveClosureAliases(appends, alias)
	return appends
}

// literalAppends is the set of names a func literal assigns, at any depth, each
// mapped to the position of its FIRST write in the literal.
//
// The position is carried for #311: a refusal an operator cannot locate is a
// disclosure they cannot act on, and for a closure the write is inside a body
// that may sit far from the call that made it provable.
func literalAppends(lit *ast.FuncLit) map[string]token.Pos {
	touched := map[string]token.Pos{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		inner, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range inner.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
				if at, seen := touched[id.Name]; !seen || id.Pos() < at {
					touched[id.Name] = id.Pos()
				}
			}
		}
		return true
	})
	return touched
}

// resolveClosureAliases folds every `g := add` edge into appends: each alias
// takes the union of what every name reachable from it along those edges
// appends to, so a chain (`g := add; h := g`) resolves however the bindings were
// written and whatever order the maps happen to iterate in.
//
// Written as a walk from each alias rather than as a repeat-until-stable pass
// over the whole edge set, so that the transitive step is a line that can be
// REMOVED and produce a deterministic failure. A stabilising pass reaches the
// same answer, but how many passes it takes depends on map iteration order, so
// removing its transitivity reddens the chain test only sometimes — a proof that
// is not reliably a proof.
//
// seen makes a cycle (`a := b; b := a`, or a name aliasing itself) terminate.
//
// Every walk READS the literal bindings and writes nowhere, and the results are
// merged in only once every walk is done. Resolving in place instead would let
// one alias's answer be reached through another alias's already-merged entry —
// the same answer, but arrived at by two routes, so removing the transitive step
// would break the chain only on the map iteration orders that take the long
// route. Keeping the two phases apart is what makes that step's removal a
// deterministic red.
func resolveClosureAliases(appends map[string]map[string]token.Pos, alias map[string][]string) {
	resolved := map[string]map[string]token.Pos{}
	for dst := range alias {
		seen := map[string]bool{dst: true}
		stack := append([]string(nil), alias[dst]...)
		reached := map[string]token.Pos{}
		for len(stack) > 0 {
			src := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[src] {
				continue
			}
			seen[src] = true
			for v, pos := range appends[src] {
				// Earliest write wins, so a union reached along two alias edges
				// anchors at one position whatever order the maps iterate in.
				if at, ok := reached[v]; !ok || pos < at {
					reached[v] = pos
				}
			}
			stack = append(stack, alias[src]...)
		}
		if len(reached) > 0 {
			resolved[dst] = reached
		}
	}
	for dst, reached := range resolved {
		if appends[dst] == nil {
			appends[dst] = map[string]token.Pos{}
		}
		for v, pos := range reached {
			if at, ok := appends[dst][v]; !ok || pos < at {
				appends[dst][v] = pos
			}
		}
	}
}

// invokedNames reports the names body PROVABLY CALLS: reached on every path
// through the block, and evaluated before a later append could be read. nil when
// there is none.
func invokedNames(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	scanInvoked(body.List, out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// scanInvoked walks the statements that provably run, searching only their
// provably-evaluated parts. The kinds absent from the switch are the ones whose
// execution this pass cannot prove, and they are enumerated in the file comment.
//
// An assignment's TARGET is searched as well as its value, for the same reason
// scanRun searches it: `m[add()] = 1` evaluates the index expression.
//
// An IIFE is recognised through iifeBody rather than re-derived, so the shape
// admitted here is the one the rest of the pass already means by "immediately
// invoked" — no parameters and no results. That exclusion earns its keep at this
// call site too: a parameter can SHADOW the closure's own name
// (`func(add func()){ add() }(other)`), and a call through a shadowed name says
// nothing about the closure this block bound.
func scanInvoked(stmts []ast.Stmt, out map[string]bool) {
	for _, st := range stmts {
		switch s := st.(type) {
		case *ast.AssignStmt:
			for _, e := range s.Lhs {
				scanCalls(e, out)
			}
			for _, e := range s.Rhs {
				scanCalls(e, out)
			}
		case *ast.ExprStmt:
			scanCalls(s.X, out)
			if lit, ok := iifeBody(st); ok {
				scanInvoked(lit.List, out)
			}
		case *ast.SendStmt:
			scanCalls(s.Chan, out)
			scanCalls(s.Value, out)
		case *ast.DeclStmt:
			for _, e := range declValues(s) {
				scanCalls(e, out)
			}
		case *ast.IfStmt:
			// Init and Cond run on every path that reaches the statement;
			// neither arm does.
			if s.Init != nil {
				scanInvoked([]ast.Stmt{s.Init}, out)
			}
			scanCalls(s.Cond, out)
		case *ast.LabeledStmt:
			// A label does not change whether the statement under it runs.
			scanInvoked([]ast.Stmt{s.Stmt}, out)
		case *ast.BlockStmt:
			// A bare block runs where it sits. See the file comment for why
			// entering one is right here and wrong in scanRun.
			//
			// ONE SHAPE PAYS FOR IT, and it is the whole cost, measured: a block
			// that shadows the query with `var q = …` and calls a closure
			// appending to the INNER q now untracks the OUTER one, losing a
			// candidate. The `q := …` spelling of the same shadow costs nothing —
			// untrackAssigned already deletes the outer q on the block itself —
			// and the `var` spelling of it INSIDE a closure body was already
			// untracked before this walk existed, because closureAppends reads
			// the appended names by bare name too. So this is the block-arm face
			// of a binding-side blindness #72 shipped with, not a new kind.
			scanInvoked(s.List, out)
		}
	}
}

// scanCalls records the bare-identifier callee of every call in e.
//
// It stops at a *ast.FuncLit: the literal's body has not run where the literal
// sits. That is what keeps a SELF-RECURSIVE literal honest — `add = func(){ q +=
// …; add() }` mentions add() inside itself, and reading that as an invocation
// would untrack on a closure the block may never call at all (`_ = add`).
//
// Only a bare identifier counts. `h.order()` and `fs[0]()` name a field and an
// element: which function runs is dataflow this pass does not have, and storing
// a closure is not evidence of calling it — `hooks{order: add}` with nothing
// ever calling h.order is the register(add) case, where the unordered locking
// SELECT is the real value.
func scanCalls(e ast.Expr, out map[string]bool) {
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				out[id.Name] = true
			}
		}
		return true
	})
}

// escapedClosureRisk maps a name a closure APPENDS TO to a rendering of the
// call that closure escaped into — `run(add)`, or the inline `run(func(){…})`.
// Empty when this block hands no appending closure to any call.
//
// THE OTHER HALF OF "A NAME HANDED TO A CALL IS NOT A CALL", and the half
// #337's own premise pays for. Noticing the escape is free; ACTING on it is
// what costs a true positive, so the notice is spent on the operator instead of
// on an untrack. NOTHING HERE UNTRACKS ANYTHING. It adds no candidate and
// removes none — fold.go stamps what this returns onto worlds it was already
// emitting, and TestNoticingTheEscapeEmitsExactlyWhatItEmittedBefore compares
// the emitted set, field by field with this one ignored, against what the tree
// emitted before this function existed.
//
// WHY IT TRAVELS WITH THE CANDIDATE. `run(add)` and a genuinely unordered
// locking SELECT reach the gate as the same text at the same position: the
// first is a false positive on code ordered on every real path, the second is
// the deadlock hazard the rule exists for. A disclosure an adopter can only
// find by reading Go source inside internal/sqlextract is not a disclosure, so
// the spelling has to leave the package attached to the thing it is about.
//
// EXCLUDED, deliberately: a name invokedNames already proves is called. What
// this map answers is "which variables have an escape whose appends are missing
// from the world the fold emits", and for a provably-called closure #72 removed
// that world — closureWritten reads the SAME invokedNames over the SAME
// closureAppends, so every variable such a closure touches is in unfoldable and
// is never tracked at all. Recording one would make the map assert something
// false about its own subject.
//
// THE EXCLUSION IS THEREFORE AN EQUIVALENT MUTANT, and it is said plainly here
// rather than left to look covered: because those two functions read the same
// two sets, no candidate exists for the excluded names, so removing the test
// changes no output and no assertion in this package can redden on it. It stays
// for what the map MEANS, on the model of the occurrences[text] term in
// fold.go's emit — a guard that looks covered and is not is worse than one
// whose limit is written down.
//
// THE INLINE SPELLING IS THE SAME HAZARD, so one predicate covers both.
// `run(func(){ q += … })` hands the same closure to the same callee without
// ever naming it, and fold_iife_test.go's LOAD-BEARING GUARD pins it folding.
// Flagging the named spelling alone would leave the anonymous one as a second
// undisclosed false positive of exactly the shape this work exists to close —
// and it is the spelling the guard says must keep folding, so it is the one an
// adopter is likelier to meet.
//
// EVERY CALL IN THE BLOCK COUNTS, at any depth, and that is the conservative
// direction here rather than an oversight. scanInvoked walks only what provably
// RUNS because untracking on a maybe deletes a true positive; nothing is
// untracked here, so a conditional or scheduled escape (`if b { run(add) }`,
// `defer run(add)`) costs one sentence on a candidate that is emitted either
// way, while missing one costs an operator who cannot recognise the finding
// they are holding.
//
// WHAT IT DOES NOT SEE, named rather than implied: a closure that leaves
// through anything other than a call argument — `hooks{order: add}` then
// `h.order()`, `fs := []func(){add}` then `fs[0]()`. Those keep folding, for
// the reason everything here keeps folding, and they carry no disclosure. They
// are the same hazard one spelling further out and the residue is real;
// TestClosureNotProvablyCalledStillFolds is where their behaviour is pinned.
//
// DETERMINISTIC BY CONSTRUCTION. Two escapes touching one variable resolve to
// the EARLIEST, compared by position rather than by which one the walk reached
// first, so the spelling an operator is shown cannot turn on how a map happened
// to iterate.
func escapedClosureRisk(body *ast.BlockStmt) map[string]string {
	appends := closureAppends(body)
	invoked := invokedNames(body)
	spelling := map[string]string{}
	at := map[string]token.Pos{}
	note := func(touched map[string]token.Pos, call *ast.CallExpr, argText string) {
		for name := range touched {
			if prev, seen := at[name]; seen && prev <= call.Pos() {
				continue
			}
			spelling[name] = calleeSpelling(call.Fun) + "(" + argText + ")"
			at[name] = call.Pos()
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			switch a := ast.Unparen(arg).(type) {
			case *ast.Ident:
				if !invoked[a.Name] {
					note(appends[a.Name], call, a.Name)
				}
			case *ast.FuncLit:
				note(literalAppends(a), call, litSpelling(a))
			}
		}
		return true
	})
	return spelling
}

// calleeSpelling renders the callee of an escaping call.
//
// A RENDERING, NOT A REPRODUCTION, and where it cannot render it says so. A
// callee that is an element or a returned value — `fs[0](add)` — names no
// function this pass resolves, so it renders as `…` rather than as a guess: the
// argument is the half that identifies the closure, and inventing a callee name
// would send its reader looking for a line that does not exist.
func calleeSpelling(fun ast.Expr) string {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return calleeSpelling(f.X) + "." + f.Sel.Name
	}
	return "…"
}

// litSpelling renders an inline closure written where the argument goes.
//
// The signature is rendered as empty ONLY when it is empty. A literal taking
// parameters or returning is written `func(…){…}`, for calleeSpelling's reason:
// a rendering showing a signature the source does not have is a rendering its
// reader cannot find.
func litSpelling(lit *ast.FuncLit) string {
	if lit.Type.Params.NumFields() == 0 && lit.Type.Results.NumFields() == 0 {
		return "func(){…}"
	}
	return "func(…){…}"
}
