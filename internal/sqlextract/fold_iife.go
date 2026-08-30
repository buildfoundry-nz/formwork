package sqlextract

import (
	"go/ast"
	"go/token"
	"strings"
)

// iifeBody reports the body of an immediately-invoked function literal this pass
// models: `func(){ … }()` with no parameters and no results, appearing as a
// STATEMENT (an *ast.ExprStmt).
//
// Position is part of the definition, not an incidental consequence of where
// foldStmts happens to call this. A literal invoked in an `if` CONDITION —
// `if func() bool { q += … }() {` — also provably runs, so "the only shape
// whose execution is provable" would be false; it is refused here on its
// result, arrives through IfStmt.Cond which no write-detection path inspects,
// and therefore fabricates in both directions exactly like a disqualified body
// (spec §9's third false positive, locking.go's item 4). This is the only
// RECOGNISED such shape, which is a smaller claim and the true one.
//
// Its execution being provable — it runs, exactly once, right here — is why
// its appends can be folded in source order with the surrounding ones and the
// reassembled value is the real one. A
// conditionally-called, never-called, or created-after-use closure has no such
// guarantee, and for those the value assembled from the appends OUTSIDE the
// closure is itself a real path (the not-called one), so the enclosing walk is
// already correct and is left alone (spec §4.2, §10).
//
// Parameters are excluded because they can SHADOW the name the body writes
// (`func(q string){ … }(q)`), and parse-only cannot tell. A NAMED result is
// excluded for the same reason — a naked `return` assigns through the
// shadowed name (`func() (q string) { … }()`). An unnamed result
// (`func() string { … }()`) cannot shadow this way and is excluded anyway,
// rather than carving out a narrower rule for the one shape that cannot
// fabricate.
//
// A true result names only the SHAPE as inlineable. foldStmts additionally
// requires iifeModellable(body) before actually folding it: a body containing
// a construct this pass cannot fold in place is left opaque instead, exactly
// as every closure was before this task, rather than partially modelled.
func iifeBody(st ast.Stmt) (*ast.BlockStmt, bool) {
	es, ok := st.(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := ast.Unparen(es.X).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	fl, ok := ast.Unparen(call.Fun).(*ast.FuncLit)
	if !ok {
		return nil, false
	}
	if fl.Type.Params != nil && len(fl.Type.Params.List) > 0 {
		return nil, false
	}
	if fl.Type.Results != nil && len(fl.Type.Results.List) > 0 {
		return nil, false
	}
	return fl.Body, true
}

// iifeModellable reports whether an IIFE's body can be folded INLINE into the
// enclosing scope: every statement is one of a small admitted set
// (iifeShapesAdmissible), and no name the body binds is also one it appends to
// (iifeShadowsAnAppend).
//
// THE INVARIANT, which is the whole soundness argument: INSIDE AN INLINED BODY
// NO STATEMENT MAY UNTRACK AN OUTER VARIABLE, none may be folded INTO one it
// does not really write, and none may leave the value UNPARSEABLE where base
// left it readable. Every statement therefore satisfies exactly one of
//
//   - it provably cannot write a variable of the enclosing scope, and is
//     SKIPPED — neither tracked nor untracked;
//   - it is an append this pass can render as real text, and is FOLDED; or
//   - neither holds, and it DISQUALIFIES the whole body, so the literal is left
//     opaque and this walk reproduces base byte for byte.
//
// The fold arm is not a formality. An append is the one statement this walk
// takes ACTION on, and rendering it can lose more than it gains: an append with
// no literal text anywhere becomes a bare placeholder glued to the accumulated
// value, and the unparseable result is dropped in silence where base's coarser
// value still reached the gate (iifeAssignAdmissible).
//
// Untracking is never a third arm. It reads as the conservative move and is the
// opposite: foldStmts and foldAssign delete by BARE NAME with no scope stack,
// so an untrack reached from inside an inlined body deletes the ENCLOSING
// scope's variable — routinely one the closure provably never wrote — and every
// later append to it with it. What is emitted then is not a smaller set of
// worlds, it is nothing at all: the unordered locking SELECT base reported
// reports NOTHING, which in a deadlock gate is the failure nobody sees.
//
// FOUR rounds of review each found another statement reaching an untrack from
// inside an inlined body, and each was patched arm by arm until the shape of
// the mistake was clear: the predicate kept RE-DERIVING what foldStmts does
// instead of being provably bounded by it — the same two-answers-to-one-
// question failure #72 itself was about, reproduced inside its own fix. Both
// halves below are written to close a CLASS rather than the instances of it,
// and each names the untrack site it makes unreachable.
func iifeModellable(stmts []ast.Stmt) bool {
	return iifeShapesAdmissible(stmts) && !iifeShadowsAnAppend(stmts)
}

// iifeShapesAdmissible reports whether every statement in an IIFE's body is one
// of three shapes: an admitted assignment (iifeAssignAdmissible), an if with
// NEITHER an Init NOR an Else (recursing into its Body), or another recognised
// IIFE (recursing via iifeBody). Anything else — a return, a loop, a
// switch/select, a bare block, a declaration, a go or defer, a plain call, an
// if WITH an Init, an if WITH an Else — makes the whole body unmodellable.
//
// With only these admitted, every untrack foldStmts owns is unreachable from an
// inlined body BY CONSTRUCTION, not by inspection of what each happens to do
// today:
//   - the Init untrack (`if s.Init != nil { untrackAssigned(s.Init, sc) }`)
//     never runs, because an accepted if always has Init == nil;
//   - the mandatory-if/else untrack (`untrackAssigned(s, sc)` when
//     Else != nil) never runs, because an accepted if always has Else == nil;
//   - the catch-all `default: untrackAssigned(st, sc)` never runs, because
//     every accepted statement is explicitly one of the three shapes;
//   - foldAssign's own untracks never run, because it skips every admitted
//     shape but `+=` while g.shadow holds.
//
// The first three each cost a review round to find, and each looked foldable
// until foldStmts' actual handling was read: an if WITH an else is not recursed
// into at all but untracked WHOLESALE (spec §4.2), and an if's Init is
// untracked UNCONDITIONALLY even though `if q := "x"; q != ""` binds q in the
// if statement's OWN scope. Applied inside an inlined body either deletes the
// outer q outright (TestFromGoReassembledIIFEMandatoryIfElseLeavesLiteralOpaque,
// TestFromGoReassembledIIFEIfWithInitLeavesLiteralOpaque, and their gate
// counterparts).
//
// This is deliberately all-or-nothing rather than per-statement. Once a body is
// inlined its statements share the ENCLOSING scope, so a construct outside the
// admitted set risks touching a variable no real path there touches; and a
// `return` is worse still, making every statement after it conditional in a way
// this linear walk cannot represent at all, so folding past one does not just
// miss a world, it fabricates one. Refusing the whole body is what keeps a
// disqualified shape provably identical to pre-#72 behaviour instead of
// reasoning per statement about which unmodelled constructs are "safe" —
// reasoning that has already cost four rejected designs in this file (spec §10).
func iifeShapesAdmissible(stmts []ast.Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *ast.AssignStmt:
			if !iifeAssignAdmissible(s) {
				return false
			}
		case *ast.IfStmt:
			if s.Init != nil || s.Else != nil {
				return false // untracked (wholly or via Init), not folded
			}
			if !iifeShapesAdmissible(s.Body.List) {
				return false
			}
		default:
			body, ok := iifeBody(st)
			if !ok || !iifeShapesAdmissible(body.List) {
				return false
			}
		}
	}
	return true
}

// iifeAssignAdmissible reports whether one assignment inside a would-be inlined
// body is one the walk may take — it provably cannot write a variable of the
// ENCLOSING scope, or it is an append this pass can render as real text — one
// token at a time.
//
//   - `:=` binds anew in the block it is written in, and every block here is
//     inside the literal, so no number of LHS names can reach an outer
//     variable. Admitted, and SKIPPED by foldAssign — the outer variable of the
//     same name is left tracked, because the closure never touched it.
//   - `+=` is the append this pass exists to fold, and the one token the skip
//     guard passes through, so two things are checked. First the arity foldAssign
//     folds: ONE target, ONE value. go/parser accepts `a, q += "1", "2"` — a
//     type error, not a parse error — and that lands in foldAssign's
//     multi-LHS/RHS arm, which deletes every ident on the Lhs. This pass never
//     type-checks its input, so "it would not compile" is not a reason to let
//     an untrack through; the arity check is what makes "foldAssign performs no
//     delete at all while g.shadow holds" true of EVERY input.
//     Second, what the RHS RENDERS TO must carry real content BEFORE its first
//     `fw_expr` placeholder. The position is the point, not a refinement of it.
//     The fold concatenates an append onto the accumulated value with no
//     separator, so a placeholder standing first is glued to that value's last
//     token: `…WHERE s = 'x'` + `fw_expr` is one token no query contains, and
//     the parse has already failed to the LEFT of the placeholder — nothing
//     after it can rescue that, which is why `fw_expr ASC` and
//     `fw_expr ORDER BY id` are refused despite carrying plenty of text. Give
//     the placeholder literal text of its own to sit behind and it lands in a
//     column position instead, where it parses: ` ORDER BY fw_expr` is the
//     common shape, and it is how a lock or an order added inside a closure is
//     seen at all (TestLockingIIFEContentBeforePlaceholderLockStillFires).
//     Refused, then, are `q += buildClause()`, `q += clause`, `q += parts[0]`,
//     `q += "" + col`, `q += fmt.Sprintf("%s", col)` and the two leading-
//     placeholder forms above: each renders to a value the fold cannot make
//     parseable, so the candidate is dropped without a word, where base — the
//     literal left opaque — emitted a coarser candidate that DOES parse and the
//     gate reported the lock. Folding honestly there trades a reported deadlock
//     hazard for silence. Refusing the body restores base exactly — that part
//     is unconditional — but "costs no coverage" overstates it: this check
//     reads the OPERAND alone, never what it is about to be glued onto, so it
//     cannot distinguish a placeholder landing after a completed literal
//     (`'x'fw_expr`, unparseable) from the same placeholder landing right
//     after a bare comparison operator (`s = fw_expr`, which parses —
//     measured). Before round 5 added this check, a bare `q += clause` glued
//     onto a seed ending `s = ` folded, parsed, and fired the gate; the
//     shipped check refuses that shape too, uniformly, because telling the
//     two apart needs the accumulated foldVar this predicate never sees — it
//     stays a pure function of the closure's own AST, like every other check
//     in this function. The real trade: a strip of coverage given up for a
//     check that never reaches outside the statement it is judging.
//     The RENDERING is what is tested, not reassembleOperand's bool: that bool
//     reports PROVENANCE — was there a literal token — and an empty literal or
//     a bare `%s` is a token carrying nothing, so it read true for appends that
//     render to a bare placeholder. Whitespace is not content either, which was
//     measured rather than assumed: ` fw_expr`, `fw_expr `, and "\tfw_expr"
//     each parse as nothing, exactly like the placeholder alone — a separator
//     cannot give it a syntactic home
//     (TestFromGoReassembledIIFEPlaceholderOnlyAppendLeavesLiteralOpaque,
//     TestFromGoReassembledIIFELeadingPlaceholderAppendLeavesLiteralOpaque).
//   - `=` writes variables that already exist, so a named target might be the
//     outer one — `func(){ if b { q = "z" } }()` really writes it when b — and
//     parse-only cannot tell that from a shadow. Admitted only when NO target
//     is a named identifier: `_ = q` and `o.f = q` write no variable this pass
//     tracks (foldAssign already returns without touching sc.vars for a
//     selector, in an inlined body and outside one alike). Any named target
//     disqualifies the body.
//   - `-=`, `*=`, `|=`, … write a variable that must already exist, so like a
//     named `=` they may reach an outer one. Disqualifying.
//
// The LHS is read through ast.Unparen: `(q) = …` is valid Go, survives gofmt,
// and is a named write — the same parenthesised-LHS blind spot that put a
// fabricated world in front of the gate with no closure involved (spec §4.2).
func iifeAssignAdmissible(s *ast.AssignStmt) bool {
	switch s.Tok {
	case token.DEFINE:
		return true
	case token.ADD_ASSIGN:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			return false
		}
		text, _ := reassembleOperand(s.Rhs[0])
		if i := strings.Index(text, fwExprPlaceholder); i >= 0 {
			text = text[:i]
		}
		return strings.TrimSpace(text) != ""
	case token.ASSIGN:
		for _, lhs := range s.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok && id.Name != "_" {
				return false
			}
		}
		return true
	}
	return false
}

// iifeShadowsAnAppend reports whether any name the body BINDS with `:=` is also
// a name it APPENDS to with `+=`. Such a body is not inlined.
//
// This is the invariant's other half, and the hazard skipping a `:=` opens.
// Skipping is right for the `:=` itself — it writes no outer variable — but
// sc.vars is keyed by BARE NAME with no scope stack, so a later `q += …` in the
// same body is folded into the OUTER q even though it appends to the
// closure-local one. That fabricates a value no path produces, and an ORDERED
// one in the shape that matters (`q := …; q += " ORDER BY id"`), which reads as
// safe and silences the outer unordered lock: base 1 finding, skip without this
// check 0 (TestFromGoReassembledIIFEShadowedAppendLeavesLiteralOpaque,
// TestLockingIIFEShadowedAppendStillFires).
//
// It is deliberately order-blind and scope-blind — a `+=` BEFORE the `:=`, or in
// a sibling block the binding never reaches, really does write the outer
// variable, and this refuses those too. That is NOT merely a precision cost:
// whenever the wrongly-disqualified body also contains a real, unconditional
// append that a correct scope analysis would keep, refusing the whole body
// drops it too, reproducing the exact #72 pair. Measured on the order-blind
// sub-case (an append before the shadowing `:=`, which Go scopes from the
// statement after it, not the whole block): an ORDER BY appended there
// fabricates an unordered value where the real one is ordered (1 finding at
// the gate); a lock appended there drops the query's only lock entirely (0
// findings on a genuinely unordered locking SELECT). The scope-blind sub-case
// (an append after an if-block whose local binding never reaches it)
// reproduces both directions identically. Deciding it properly needs the
// scope stack this pass does not have, and a wrong scope proof re-opens
// exactly that fabrication.
//
// It also keeps this inline walk disjoint from the separate walk that treats
// the same literal as its own scope root (foldAssignments): a query the closure
// both SEEDS and APPENDS is a bound-and-appended name by definition, so the
// body is disqualified here and that query is folded once, by the root walk
// alone (TestFromGoReassembledIIFEOwnQueryStillFolds).
func iifeShadowsAnAppend(stmts []ast.Stmt) bool {
	defined, appended := map[string]bool{}, map[string]bool{}
	iifeAssignedNames(stmts, defined, appended)
	for name := range defined {
		if appended[name] {
			return true
		}
	}
	return false
}

// iifeAssignedNames collects the names an admitted body binds with `:=` into
// defined and those it appends to with `+=` into appended, recursing through
// the same shapes iifeShapesAdmissible accepts (and only those — it is called
// after that predicate holds). Recursing over the WHOLE body, not one level, is
// what makes the check see a binding and an append at different depths.
func iifeAssignedNames(stmts []ast.Stmt, defined, appended map[string]bool) {
	for _, st := range stmts {
		switch s := st.(type) {
		case *ast.AssignStmt:
			into := defined
			switch s.Tok {
			case token.DEFINE: // into stays defined
			case token.ADD_ASSIGN:
				into = appended
			default:
				continue // an admitted `=` writes no named variable
			}
			for _, lhs := range s.Lhs {
				if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
					into[id.Name] = true
				}
			}
		case *ast.IfStmt:
			iifeAssignedNames(s.Body.List, defined, appended)
		default:
			if body, ok := iifeBody(st); ok {
				iifeAssignedNames(body.List, defined, appended)
			}
		}
	}
}
