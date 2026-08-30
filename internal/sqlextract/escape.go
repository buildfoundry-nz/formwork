package sqlextract

import (
	"go/ast"
	"go/token"
)

// escape.go — #310, the half of #74 that was never built: THE ADDRESS HANDED TO
// A HELPER.
//
// #74's Scope reads "writes through `*p` where `p` was taken from a tracked
// variable, and the address handed to a helper", and only the first clause
// shipped. aliasUnsafe (unseenwrite.go) answers nothing unless the same block
// also writes through a dereference, so `lockIt(&q)` — with no `*p` anywhere in
// the caller — left q fully tracked and the fold emitted a value assembled from
// only the writes it happened to see. Both directions were live: the helper
// adding the ORDER BY made the rule fire on provably safe code, the helper
// adding the LOCK made a real deadlock hazard vanish.
//
// The model is the one spec §4.1 specifies. Every `&name` in a scope is one of
// two things:
//
//   - a RESOLVABLE LOCAL ALIAS — `p := &q` (or `var p = &q`) bound at the top
//     level of the block, where every other mention of p in the body is a
//     dereference. This block's own text then decides what p names, so #74's
//     deref-write analysis governs it: `*p += …` untracks q, and a read through
//     `*p` is not a write and leaves every append visible.
//   - an ESCAPE — anything else. Handed to a call, stored in a local or a
//     field or a map, sent on a channel, put in a composite literal, or bound
//     to a pointer that is mentioned some other way. What the far side does
//     with it is out of reach of a parse-only pass (§2), so the name is
//     untracked: nothing is emitted for it, rather than something wrong.
//
// WHERE IT UNTRACKS IS THE WHOLE DESIGN, and getting it wrong re-makes the
// design spec §10 records as REJECTED — it removed ten findings, eight of them
// true positives. Under a branch the not-taken path is real, so the world
// assembled WITHOUT the callee's writes is a genuine execution path and a
// finding on it is a true positive. An escape therefore untracks only where it
// PROVABLY RUNS, which §4.1 states as two independent conditions: the enclosing
// branch context, and the position inside the statement. The second is not
// implied by the first. A loop body runs zero times on an empty slice, a
// `switch` case may match nothing, a `select` clause is one of several, a
// `defer`/`go` call has not run when the value reaches the driver, and a
// `return` expression evaluates after every append — so the value being handed
// out is exactly the one the fold assembled.
//
// Only §4.1's escape half is built here. Its other half — reading `*p op= …`
// back as `q op= …` for a resolvable alias — is not, and that is a decision
// rather than an omission: it opens a new EMISSION path (the fold would start
// claiming worlds it does not claim today) where untracking only ever removes
// one, and locking.go's shipped COVERAGE LIMIT already tells its reader that a
// pointer-mutated query is UNTRACKED, not resolved.

// escapedNames reports the names whose address escapes body at a position body
// provably reaches, mapped to the position of the escaping `&name` itself, and
// nothing else. nil when there is none, so unfoldable can keep allocating only
// when it must.
//
// The position is the `&q` (#311): that expression IS the construct an operator
// has to look at, and it is the only one of the three block-wide analyses whose
// anchor is exact rather than representative — the far side of the escape is by
// definition not in this file.
func escapedNames(body *ast.BlockStmt) map[string]token.Pos {
	out := map[string]token.Pos{}
	scanRun(body.List, resolvedAliases(body), out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// scanRun walks the statements that provably run, in the sense §4.1 requires:
// reached on every path through this list, and evaluated before a later append
// could be read. Only the provably-evaluated PARTS of each statement are
// searched.
//
// An assignment's TARGET counts, not just its value: `m[key(&q)] = 1` evaluates
// the index expression, and that statement is otherwise invisible to the whole
// walk — foldAssign returns early on an index target because it cannot be a
// tracked name.
//
// The statement kinds NOT listed here are the point of the function, so they
// are named rather than left to inference. `for`/`range` bodies, `switch` and
// `select` clauses, and any nested block run conditionally or not at all;
// `defer` and `go` schedule a call that has not run where the value is used;
// `return` evaluates after every append. A nested block is skipped for one more
// reason on top: a `q := …` inside it may SHADOW an outer q, and this
// classifier is keyed by bare name with no scope stack — the same reason
// foldStmts refuses to recurse into a bare block. Untracking an outer variable
// on the strength of an inner one's escape would delete a true positive
// silently.
//
// An `if` contributes its Init and its condition, both of which run on every
// path that reaches the statement, and neither of its arms. An escape in ONE
// arm leaves the other arm's world real; an escape in BOTH arms does not, and
// that case is a disclosed miss (locking.go's COVERAGE LIMIT says so) because
// telling the two apart means proving the two arms escape the SAME name, which
// the shadowing problem above puts out of reach here.
func scanRun(stmts []ast.Stmt, resolved map[*ast.UnaryExpr]bool, out map[string]token.Pos) {
	for _, st := range stmts {
		switch s := st.(type) {
		case *ast.AssignStmt:
			for _, e := range s.Lhs {
				scanEscapes(e, resolved, out)
			}
			for _, e := range s.Rhs {
				scanEscapes(e, resolved, out)
			}
		case *ast.ExprStmt:
			scanEscapes(s.X, resolved, out)
		case *ast.SendStmt:
			scanEscapes(s.Chan, resolved, out)
			scanEscapes(s.Value, resolved, out)
		case *ast.DeclStmt:
			for _, e := range declValues(s) {
				scanEscapes(e, resolved, out)
			}
		case *ast.IfStmt:
			if s.Init != nil {
				scanRun([]ast.Stmt{s.Init}, resolved, out)
			}
			scanEscapes(s.Cond, resolved, out)
		case *ast.LabeledStmt:
			// A label does not change whether the statement under it runs.
			scanRun([]ast.Stmt{s.Stmt}, resolved, out)
		}
	}
}

// scanEscapes records every `&name` in e that is not a resolvable alias
// binding's own operand.
//
// It stops at a *ast.FuncLit: the literal's body has not run where the literal
// sits. That is true of a closure however it is later used, and it is what
// keeps a `q := …` inside a literal body — which binds in the CLOSURE's scope —
// from untracking a variable of the enclosing one.
//
// ONLY A BARE IDENTIFIER'S ADDRESS COUNTS. `&cfg.q` and `&xs[0]` name a field
// and an element, and the fold tracks single identifiers only (fold.go's
// foldAssign returns early on a selector or index target), so no field or
// element can ever be a tracked name. Reading the identifier out of the
// selector instead of the operand would untrack a local that merely shares the
// field's name.
func scanEscapes(e ast.Expr, resolved map[*ast.UnaryExpr]bool, out map[string]token.Pos) {
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.UnaryExpr:
			if x.Op != token.AND || resolved[x] {
				return true
			}
			if id, ok := ast.Unparen(x.X).(*ast.Ident); ok {
				// Earliest escape wins: a name handed out twice is one refusal,
				// and the first hand-out is where the value stopped being ours.
				if at, seen := out[id.Name]; !seen || x.Pos() < at {
					out[id.Name] = x.Pos()
				}
			}
		}
		return true
	})
}

// aliasBinding is one candidate resolvable alias: `p := &q` or `var p = &q` at
// the top level of the block. self is the binding occurrence of p, which the
// mention test below must not count against itself.
type aliasBinding struct {
	name string
	self *ast.Ident
	addr *ast.UnaryExpr
}

// resolvedAliases returns the `&name` expressions that are a resolvable local
// alias's operand — the ones §4.1 says this block's own text decides, so they
// are NOT escapes.
//
// The test is: p is bound once at the top level of this block to the address of
// a bare identifier, and every other mention of p anywhere in the body is the
// operand of a dereference. A mention is any *ast.Ident of that name, including
// a field name in a selector (`cfg.p`) — an over-strict reading, deliberately,
// because losing the exemption costs a candidate and a wrong exemption costs
// a fabricated world. A binding inside a nested scope resolves nothing, for the
// same reason: no scope stack, so an inner `p := &other` must not decide what
// an enclosing `*p` writes.
func resolvedAliases(body *ast.BlockStmt) map[*ast.UnaryExpr]bool {
	cands := aliasCandidates(body)
	if len(cands) == 0 {
		return nil
	}
	deref := map[*ast.Ident]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if se, ok := n.(*ast.StarExpr); ok {
			if id, ok := ast.Unparen(se.X).(*ast.Ident); ok {
				deref[id] = true
			}
		}
		return true
	})
	out := map[*ast.UnaryExpr]bool{}
	for _, c := range cands {
		resolvable := true
		ast.Inspect(body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != c.name || id == c.self || deref[id] {
				return true
			}
			resolvable = false
			return false
		})
		if resolvable {
			out[c.addr] = true
		}
	}
	return out
}

// aliasCandidates collects the top-level `p := &name` and `var p = &name`
// bindings. A `var` spec is admitted on the same terms as a `:=` because the
// two say the same thing about p, and treating them differently would make the
// exemption turn on a spelling.
func aliasCandidates(body *ast.BlockStmt) []aliasBinding {
	var out []aliasBinding
	add := func(lhs, rhs ast.Expr) {
		id, ok := ast.Unparen(lhs).(*ast.Ident)
		if !ok {
			return
		}
		un, ok := ast.Unparen(rhs).(*ast.UnaryExpr)
		if !ok || un.Op != token.AND {
			return
		}
		if _, ok := ast.Unparen(un.X).(*ast.Ident); !ok {
			return
		}
		out = append(out, aliasBinding{name: id.Name, self: id, addr: un})
	}
	for _, st := range body.List {
		switch s := st.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE && len(s.Lhs) == 1 && len(s.Rhs) == 1 {
				add(s.Lhs[0], s.Rhs[0])
			}
		case *ast.DeclStmt:
			for _, spec := range declSpecs(s) {
				if len(spec.Names) == 1 && len(spec.Values) == 1 {
					add(spec.Names[0], spec.Values[0])
				}
			}
		}
	}
	return out
}

// declSpecs is the `var` value specs of a declaration statement, and nothing
// for a `const` or `type` one.
func declSpecs(s *ast.DeclStmt) []*ast.ValueSpec {
	gd, ok := s.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return nil
	}
	var out []*ast.ValueSpec
	for _, spec := range gd.Specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			out = append(out, vs)
		}
	}
	return out
}

// declValues is every initialiser expression of a `var` declaration statement,
// whatever its arity — arity narrows the ALIAS exemption, never the escape
// search.
func declValues(s *ast.DeclStmt) []ast.Expr {
	var out []ast.Expr
	for _, vs := range declSpecs(s) {
		out = append(out, vs.Values...)
	}
	return out
}
