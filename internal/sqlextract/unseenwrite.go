package sqlextract

import (
	"go/ast"
	"go/token"
)

// unseenwrite.go holds the two analyses that answer one question: which names in
// this block have writes the fold cannot see in full?
//
// A write it cannot see is dropped while the variable stays tracked, so the fold
// emits a value assembled from only the writes it happened to see — a query no
// execution path produces (spec §4.1 promises never to emit one). Both answers
// feed the same refusal: never start tracking that name.
//
//   - aliasUnsafe    #74, a write through a taken address
//   - closureWritten #72/#337, a closure bound to a name and provably called
//     (invoked.go answers which spellings of the binding and the call count)
//   - escapedNames   #310, an address that LEAVES this block (escape.go)
//
// A pure split out of fold.go, which this work pushed to 834 lines against the
// repo's own 750-line hard cap — and that cap says never widen it, because
// consumers vendoring this source enforce it. Same move internal/scan made for
// restrict.go and internal/meta for fixturecoverage.go.

// aliasUnsafe reports the names whose address is taken in body, when body also
// contains a write through a dereference (#74).
//
// A write through a taken address is not an assignment to the identifier, so
// the fold's per-statement untracking never sees it: the variable stays
// tracked, the write is dropped, and the fold emits a value assembled from only
// the writes it happened to see — a query no execution path produces. It breaks
// both ways, and the second is the dangerous one: with the unseen write adding
// the ORDER BY the fold reports an unordered locking SELECT no path holds; with
// it adding the lock, a real deadlock hazard goes unreported.
//
// DELIBERATELY OVER-APPROXIMATE, and decided at scope entry because the two
// halves live in different statements — `p := &q` and `*p += …` — so no
// per-statement pass can see both. The halves are not correlated either:
// proving `*p` aliases `q` needs pointer analysis this pass does not have, so
// an unrelated pointer write untracks a variable it could not reach.
//
// That direction is chosen. Untracking loses a candidate; fabricating loses the
// truth, in a rule whose false negative is a silenced deadlock. The narrowing
// that stops it deleting ordinary true positives is the deref-write condition:
// taking an address to READ through (`p := &q; _ = len(*p)`) leaves every
// append visible, and still folds.
//
// THIS ANALYSIS ANSWERS ONE SPELLING ONLY, and #310 is what taught us to say so
// here rather than in a spec: `&q` handed to a helper, stored, or otherwise
// escaping the block is not a dereference, so nothing below sees it. escape.go
// answers that, and it is the reason "passing &q to a reader" no longer folds —
// a parse-only pass cannot tell a reader from a writer at a call boundary.
//
// THE POSITION IT RETURNS IS THE DEREFERENCE WRITE, not the address-take, and
// that is the honest anchor (#311): the write is the composition step this pass
// cannot read, and it is what an operator handed a "could not be read" line has
// to go and look at. The two halves are uncorrelated, so the FIRST deref write
// in the block is used for every name — naming a particular one would claim a
// correspondence this analysis does not have.
func aliasUnsafe(body *ast.BlockStmt) map[string]token.Pos {
	addrTaken := map[string]bool{}
	derefWrite := token.NoPos
	noteWrite := func(p token.Pos) {
		if derefWrite == token.NoPos || p < derefWrite {
			derefWrite = p
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if x.Op == token.AND {
				if id, ok := ast.Unparen(x.X).(*ast.Ident); ok {
					addrTaken[id.Name] = true
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if _, ok := ast.Unparen(lhs).(*ast.StarExpr); ok {
					noteWrite(lhs.Pos())
				}
			}
		case *ast.IncDecStmt:
			if _, ok := ast.Unparen(x.X).(*ast.StarExpr); ok {
				noteWrite(x.Pos())
			}
		}
		return true
	})
	if derefWrite == token.NoPos {
		return nil
	}
	out := make(map[string]token.Pos, len(addrTaken))
	for name := range addrTaken {
		out[name] = derefWrite
	}
	return out
}

// closureWritten returns the names a PROVABLY-CALLED closure appends to (#72,
// #337).
//
// untrackAssigned stops at *ast.FuncLit, so a closure's appends are neither
// folded nor untracked: the variable stays tracked, the write is dropped, and
// the fold emits a world assembled from only the appends outside the closure —
// one no execution path produces.
//
// Keyed on the closure being INVOKED, not on its existence, and that narrowing
// is the whole point. A closure that is never called, called conditionally, or
// created after the value is used makes the outside-appends world a REAL path,
// and emitting it is correct — the package doc says so. Untracking those would
// delete true positives, including the exact shape this rule exists to catch:
// a locking SELECT with no ORDER BY that the code genuinely produces.
//
// An immediately-invoked literal is NOT untracked here. A modellable one is
// folded inline by iifeBody, which is strictly better than untracking, and a
// disqualified one is already left opaque with its variable untracked. A call
// INSIDE either one is a different question, and invoked.go answers it: the
// literal provably runs, so a closure it calls provably runs too.
//
// WHICH SPELLINGS COUNT is invoked.go's whole subject, and #337 is what taught
// us to keep the question in one place. This function used to answer it inline,
// reading exactly one binding spelling (an *ast.AssignStmt) and exactly one call
// spelling (an *ast.ExprStmt at the top of body.List whose Fun is a bare ident).
// `var add = func(){ … }`, `{ add() }`, `g := add; g()`, `func(){ add() }()` and
// `s := add()` were each the same unconditional call written differently, and
// each fabricated. closureAppends and invokedNames answer the two halves for the
// whole class; the narrowing that used to live in the ExprStmt loop lives in
// scanInvoked, which walks only the statements the block provably runs.
//
// THE POSITION IT RETURNS IS THE CLOSURE'S APPEND (#311). The call is what makes
// the append real, but the append is the composition step that went unread, and
// it is the line that tells an operator what the query actually says. Earliest
// wins where a closure appends more than once, so the anchor does not depend on
// map iteration order.
func closureWritten(body *ast.BlockStmt) map[string]token.Pos {
	appends := closureAppends(body)
	if len(appends) == 0 {
		return nil
	}
	written := map[string]token.Pos{}
	for name := range invokedNames(body) {
		for v, pos := range appends[name] {
			if at, seen := written[v]; !seen || pos < at {
				written[v] = pos
			}
		}
	}
	if len(written) == 0 {
		return nil
	}
	return written
}

// unfoldable is every name in body whose writes this pass cannot see in full:
// the alias-unsafe set (#74), the provably-called-closure set (#72/#337) and the
// escaped-address set (#310). One map, because the refusal is the same — never
// start tracking it — and only the reason differs.
//
// THE REASON IS CARRIED, not discarded, and that is #313's half of this
// function. The three analyses overlap — `p := &q; *p += …; lockIt(&q)` is
// found by two of them — so the answer has to be deterministic or a consumer
// disclosing it reports a different reason run to run. The order below IS the
// answer: first match wins, most specific first. A dereference write names the
// write itself; a called closure names the code that performs it; an escaping
// address names only that the address left, which is what is left to say when
// neither of the others applies.
func unfoldable(body *ast.BlockStmt) map[string]unread {
	var out map[string]unread
	for _, found := range []struct {
		names  map[string]token.Pos
		reason UntrackReason
	}{
		{aliasUnsafe(body), reasonDerefWrite},
		{closureWritten(body), reasonCalledClosure},
		{escapedNames(body), reasonAddressEscape},
	} {
		for name, pos := range found.names {
			if out == nil {
				out = map[string]unread{}
			}
			if _, seen := out[name]; !seen {
				out[name] = unread{reason: found.reason, pos: pos}
			}
		}
	}
	return out
}
