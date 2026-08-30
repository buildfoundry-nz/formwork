package sqlextract

import (
	"go/ast"
	"go/token"
	"strconv"
)

// untrack.go holds the fold's PER-STATEMENT untracking: the answer to "this
// statement writes a tracked variable in a way foldStmts does not model, so the
// variable must stop being tracked here".
//
// Its sibling unseenwrite.go answers the other, block-wide question — which
// names must never START being tracked, because a write to them is invisible
// everywhere (a taken address, a called closure). This file is reached from
// foldStmts' `default:` arm, once per unmodelled statement, and its verdict is
// local: the variable was fine until this statement and is untracked from here.
//
// A pure split out of fold.go, which sits against the repo's own 750-line hard
// cap (.formwork/rules/file-size.yaml) — the same move unseenwrite.go made.

// untrackAssigned removes from sc every tracked variable unmodelledWrites names,
// and records each removal as a Site. Applying the verdict is all it does; which
// names and why is that function's question, and the split exists so the WHY is
// a value rather than a comment.
//
// A SITE IS EMITTED ONLY WHERE A TRACKED VARIABLE IS ACTUALLY REMOVED (#311).
// unmodelledWrites names every ident an unmodellable construct writes, most of
// which this scope never tracked — a loop counter, an error, a name bound in
// some other block — and a census listing those is a list of the repo's
// assignments rather than of the SQL gate's coverage gaps.
//
// The tracked test below is how that is enforced AND is what keeps a nil out of
// the line under it, so no mutation of it can change an output: written as a nil
// check instead it is the same function, and dropped entirely it panics.
// siteSink.add's empty-text refusal is the assertion with observable behaviour
// behind it (sites_internal_test.go pins that one), and this is said plainly
// because a guard that looks pinned and is not is worse than one whose limit is
// written down.
func untrackAssigned(node ast.Node, sc *foldScope) {
	if len(sc.vars) == 0 {
		return // nothing tracked: skip the subtree walk (the common case)
	}
	for name, u := range unmodelledWrites(node, sc.arrays) {
		v, tracked := sc.vars[name]
		if !tracked {
			continue
		}
		sc.sites.add(u.pos, u.reason, v.seed)
		delete(sc.vars, name)
	}
}

// unmodelledWrites names every variable node writes through a construct this
// walk does not model, each with the UntrackReason that says which construct.
// It does not descend into a nested function literal — an opaque boundary for
// TRACKING, since what a closure appends is unmodelled either way.
//
// EACH REASON CARRIES THE POSITION OF THE WRITE, so the refusal an operator
// reads names the statement that caused it rather than the query it was about
// (#311, sites.go). A `for` header and its body, a switch arm, a labelled
// statement — the construct is what has to be looked at, and it is routinely
// nowhere near the seed.
//
// It is separate from untrackAssigned, which does nothing but apply it, because
// the REASON is the part two other places have to agree with — the COVERAGE
// LIMIT block in internal/rules/sqlparse/locking.go and the census that reports
// unanalysable compositions — and a reason derived from the classifier cannot
// drift out of step with them the way a comment did (#313, coverage.go).
//
// TWO NODE SHAPES WRITE A VARIABLE BY NAME, not one, and they get different
// reasons because they are different disclosures. An *ast.AssignStmt is the
// obvious one. The other is a `for … = range …` clause, whose write lives in
// RangeStmt.Key/Value with RangeStmt.Tok and reaches no AssignStmt node at all —
// so before #314 it was seen by nothing here, the variable stayed tracked with
// the loop's overwrite dropped, and the fold emitted a value assembled from only
// the writes it happened to see.
//
// The range arm is deliberately NOT symmetric with the AssignStmt one, and
// rangeNonEmpty carries the reason: over a source that may iterate zero times
// the loop is a path on which the variable is NOT written, so the world built
// from the appends outside it is real and untracking would delete a true
// positive (#72's third comment, which put zero-iteration for/range out of
// scope after measuring that cost at 8 true positives).
func unmodelledWrites(node ast.Node, arrays map[string]bool) map[string]unread {
	out := map[string]unread{}
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
					out[id.Name] = unread{reason: reasonUnmodelledWrite, pos: id.Pos()}
				}
			}
		case *ast.RangeStmt:
			// token.DEFINE binds a fresh name in the loop's own scope, so the
			// tracked variable of THIS scope is never written; leave it alone.
			if x.Tok != token.ASSIGN || !rangeNonEmpty(x.X, arrays) {
				return true
			}
			// ast.Unparen because `for (q) = range …` is valid Go and survives
			// gofmt. Spec §4.2 credits foldAssign's Unparen with covering that
			// spelling; it never could — a RangeStmt does not reach foldAssign.
			for _, target := range []ast.Expr{x.Key, x.Value} {
				if id, ok := ast.Unparen(target).(*ast.Ident); ok {
					out[id.Name] = unread{reason: reasonRangeClause, pos: id.Pos()}
				}
			}
		}
		return true
	})
	return out
}

// rangeNonEmpty reports whether a range source PROVABLY iterates at least once,
// which is the condition under which the clause's write is one the fold must
// respect. It is the whole narrowing of #314's fix, so it is worth being exact
// about what it is for.
//
// Untracking every `= range` clause is the design #72's first comment proposed
// and its third comment retracted: over a map, slice, channel or iterator
// function the empty case is a REAL execution path, one on which the variable
// survives the loop, so the value the fold assembles from the appends around it
// is genuinely produced and a finding on it is a true positive. Deleting those
// was measured at 10 findings removed, 8 of them true positives.
//
// Where the source cannot be empty that path does not exist: the loop certainly
// overwrites the variable, so a world built on the pre-loop seed is a query no
// execution path produces — the #72/#73/#74 wrong-emit class. Every shape below
// is decided from SYNTAX alone, because this pass resolves no types (spec §2),
// and each is non-empty for a reason that no assignment elsewhere can undo:
// an array's length is part of its type, and a literal's is written in it.
//
// The string and int arms answer sources that cannot reach a tracked variable in
// well-typed Go — those clauses yield an index, a rune or an int, and the fold
// tracks only names seeded with a string literal. They are answered because this
// is a question about the SOURCE, and a pass that resolves no types must not
// answer one by reasoning about what the target's type would have to be;
// TestRangeNonEmptyDecidesOnSourceShapeAlone pins them, since no fixture can.
func rangeNonEmpty(src ast.Expr, arrays map[string]bool) bool {
	switch x := ast.Unparen(src).(type) {
	case *ast.Ident:
		return arrays[x.Name]
	case *ast.CompositeLit:
		// Elements first, then the type: `[2]string{}` writes no element and
		// still iterates twice, while `[...]string{"a"}` has no written length.
		// A non-empty SLICE literal counts here and does not count through a
		// name (arrayValueNonEmpty): written in the clause there is no statement
		// between it and the loop to empty it.
		return len(x.Elts) > 0 || positiveArrayLen(x.Type)
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING:
			// The quotes are part of Value, and a raw string may hold anything;
			// unquoting answers both, and an unquotable literal answers nothing.
			s, err := strconv.Unquote(x.Value)
			return err == nil && s != ""
		case token.INT:
			// Base 0 reads 0x/0o/0b and digit separators, i.e. every spelling Go
			// accepts. A value too large for an int64 is a compile error at the
			// range, so refusing it costs nothing.
			n, err := strconv.ParseInt(x.Value, 0, 64)
			return err == nil && n > 0
		}
	}
	return false
}

// arrayValueNonEmpty reports whether e is a composite literal of ARRAY type that
// iterates at least once — `[2]string{}` and `[...]string{"a"}` yes.
//
// A SLICE literal is not one, however many elements it carries, and that is the
// distinction the whole name path turns on: this predicate answers what a NAME
// is bound to, and a name bound to a slice can be assigned an empty one before
// the loop by a statement this pass does not order. An array name cannot —
// the length is in the type. Used directly as a range source a non-empty slice
// literal IS provable, which is why rangeNonEmpty reads its elements too.
func arrayValueNonEmpty(e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	if !ok {
		return false
	}
	arr, ok := ast.Unparen(lit.Type).(*ast.ArrayType)
	if !ok || arr.Len == nil {
		return false // not a literal of array type: a slice, map or struct
	}
	// Two ways an array literal has a length: written in the type, or (for
	// `[...]T`) counted from the elements.
	return positiveArrayLen(lit.Type) || len(lit.Elts) > 0
}

// positiveArrayLen reports whether typ is an array type whose length is a
// positive integer literal — `[2]string` yes, `[0]string` no (it iterates zero
// times), `[]string` no (Len is nil: a slice, whose length is a runtime fact),
// `[n]string` no (a named constant is a value this pass does not resolve), and
// `[...]string` no (the length is the element count, which the caller reads
// instead).
func positiveArrayLen(typ ast.Expr) bool {
	arr, ok := ast.Unparen(typ).(*ast.ArrayType)
	if !ok || arr.Len == nil {
		return false
	}
	lit, ok := ast.Unparen(arr.Len).(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return false
	}
	n, err := strconv.ParseInt(lit.Value, 0, 64)
	return err == nil && n > 0
}

// fixedArrays names every variable in scope here that is an ARRAY: declared by
// `var x [N]T` with N a positive integer literal, carrying the array type on the
// value instead (`var x = [2]string{}`, `x := [...]string{"a"}`), named by the
// signature as a parameter or a named result, or assigned an array literal —
// which is the only evidence a closure has of an array it captured.
//
// One property, three spellings, deliberately not three cases of "looks
// non-empty": what makes a NAME a provable range source is that its length is
// part of its TYPE, so no later assignment can shorten it and this pass — which
// orders no statements — still knows the loop runs. A name bound to a non-empty
// SLICE literal has no such guarantee and is not collected.
//
// Decided once at scope entry, like unseenwrite.go's analyses, because the
// declaration and the loop are different statements. KEYED BY NAME, so a
// same-named non-array in a sibling block is treated as the array too: the cost
// is untracking a variable whose range source might have been empty, i.e. one
// lost candidate, and it takes both a name collision and a range clause writing
// the tracked query string to reach. Nothing here descends into a function
// literal — every literal body is its own fold scope with its own map.
func fixedArrays(sig *ast.FuncType, body *ast.BlockStmt) map[string]bool {
	var arrays map[string]bool
	add := func(id *ast.Ident) {
		if arrays == nil {
			arrays = map[string]bool{}
		}
		arrays[id.Name] = true
	}
	// The signature declares names the body never binds: a parameter and a named
	// result are arrays as fixed as any local. Results is nil for a func with
	// none, which is most func literals.
	for _, list := range []*ast.FieldList{sig.Params, sig.Results} {
		if list == nil {
			continue
		}
		for _, field := range list.List {
			if !positiveArrayLen(field.Type) {
				continue
			}
			for _, name := range field.Names {
				add(name)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ValueSpec:
			for i, name := range x.Names {
				if positiveArrayLen(x.Type) || (i < len(x.Values) && arrayValueNonEmpty(x.Values[i])) {
					add(name)
				}
			}
		case *ast.AssignStmt:
			// `=` counts as well as `:=`, and not for symmetry: assignability
			// makes `x = [2]string{}` proof that x IS a [2]string, whoever
			// declared it. That is the only evidence a closure has of an array it
			// captured, whose declaration sits in an enclosing fold scope this
			// walk never reaches. Values are paired by index and a missing one is
			// skipped, which handles `a, b := f()` without a special case — a call
			// is not a composite literal.
			for i, lhs := range x.Lhs {
				id, ok := ast.Unparen(lhs).(*ast.Ident)
				if ok && i < len(x.Rhs) && arrayValueNonEmpty(x.Rhs[i]) {
					add(id)
				}
			}
		}
		return true
	})
	return arrays
}
