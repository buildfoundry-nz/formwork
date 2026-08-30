package sqlextract

import (
	"go/ast"
	"go/token"
	"sort"
)

// THE ASSIGNMENT-FLOW FOLD (#36). FromGoReassembled's single AST walk folds
// string variables composed across statements — `q := …` then `q += …`,
// including `q += …` inside an `if` branch — into candidates, treating every
// func and func-literal body as a scope root and calling foldBlock on it.
// This block is the contract for everything below it.
//
// (It used to document a foldAssignments dispatcher that owned a SECOND full
// traversal of the file. #45 merged that walk into FromGoReassembled's; the
// dispatcher is gone and the contract it documented is not.) Emission is bounded all-or-nothing over optional
// (branch-guarded) appends: each tracked variable yields its `full` (every
// append applied) and `base` (only unconditional appends applied) values, plus
// the two branch worlds of ONE complementary guard where it has one (see
// foldWorlds); the spec is
// docs/specs/2026-07-29-sqlextract-assignment-flow-folding-design.md
// §4. Only values that differ from the bare seed are emitted — the expression
// walk already emits the seed literal — and each is anchored at the seed RHS
// expression's line (the `:=` line for normal single-line code), matching the
// expression walk's own anchor so a seed/fold duplicate lands on one line and
// is caught by callers' (Line,Message) dedup.
//
// Every func and func-literal body is an independent scope root. An
// IMMEDIATELY-INVOKED literal (`func(){ … }()`) IN STATEMENT POSITION is
// additionally folded INLINE in
// its enclosing block, in source order, WHEN ITS BODY IS FULLY MODELLABLE
// (iifeModellable: every statement is one of three admitted shapes, AND no name
// it binds is also one it appends to, spec §4.2) — it provably runs, once,
// right there, so its appends belong in the surrounding value (iifeBody).
// A body containing anything else — a plain function call is the commonest
// case — is left exactly as opaque as every closure was before this task: this
// pass cannot safely represent what a disqualified body does, so nothing inside
// it is folded while the variable it closes over stays tracked outside.
//
// Any nested literal that is NOT an IIFE at all is opaque for a different
// reason: for a closure that is conditionally called, never called, or created
// after the value is used, the value assembled from the appends OUTSIDE it is
// the not-called path — a real one, so emitting it is correct. THOSE THREE ARE
// NOT ALL OF THEM, and the fourth is #337: `run(add)` against
// `func run(f func()) { f() }` is none of the three — `run` invokes the closure,
// so every real path orders the query and the outside-only world fabricates,
// §9's fourth false positive. It is emitted anyway, DECIDED rather than missed:
// to a pass that never resolves a callee that program is the same text as
// `register(add)` against `func register(f func()) {}`, where nothing runs the
// closure and the outside-only world IS the value, so untracking on the escape
// would delete that finding to delete this one. What the pass does instead is
// notice — the candidate carries ClosureEscapeRisk (invoked.go, sqlextract.go)
// so the finding built on it can name the escape, and docs/reference.md's Known
// limits carries the program and the two-outcome check for an adopter holding
// one. That reasoning
// does not cover a disqualified IIFE, which runs unconditionally right where it
// sits; what is safe there is only that its OWN appends were never folded in —
// not, as for a genuine non-IIFE closure, that the outside-only value is itself
// some other real path.
//
// Calling the IIFE case "unmodeled" was #72. It was not a miss: the appends were
// dropped while the variable stayed TRACKED, so the fold emitted a value no
// execution path produces. That fabrication is fixed for a MODELLABLE IIFE; a
// disqualified one can reproduce it exactly whenever its own body would have
// appended to the query (spec §9's false-positive list, item two). A named
// closure called unconditionally — `add := func(){ … }` then `add()` — did the
// same until #72, and does not now: invoked.go asks which statements the block
// PROVABLY RUNS, so every spelling of that call this pass can SEE untracks the
// variable and emits nothing at all (spec §9 item 2, #72/#337). The one
// spelling it cannot see is the escape above.
//
// A tracked variable assigned inside any construct this pass does not model is
// untracked — sound: a later `+=` adds to nothing (a miss, never a wrong emit).
// The guarantee is scoped to writes through the NAME. A write through a taken
// address is not one, so it is answered by refusing to track: a dereference
// write in the block (#74) and an address that ESCAPES the block where it
// provably runs (#310, escape.go) both untrack. What the far side of an escape
// writes stays out of reach — an honest non-analysis, not a wrong emit.
//
// EVERY ONE OF THOSE REFUSALS IS RECORDED, in the sink this scope carries
// (#311, sites.go), anchored at the write it could not read and carrying the
// variable's seed. A refusal emits nothing, which reads exactly like a pass, so
// the operator-facing channel that says otherwise is the only thing standing
// between "not analysed" and "clean". The two constructs that drop appends
// WITHOUT untracking — a disqualified IIFE and a literal invoked in an `if`
// condition — are recorded too, under a reason that says the query was read in
// part rather than not at all.

// guardRef is one condition an append is subject to: the path it reads (`a`,
// `o.Ordered`) and whether the branch takes it negated. A world may fix one
// truth value per path.
type guardRef struct {
	path    string
	negated bool
}

// foldSeg is one append applied to a tracked variable, in source order.
// optional marks an append inside an if-without-else branch (it is absent from
// the base world). guards holds the conditions that fire it, outermost first —
// a conjunction, since a nested append needs every enclosing branch taken.
// opaque marks an optional append at least one of whose enclosing conditions is
// not a usable proposition (a call, a compound expression, an if-Init binding),
// so its guards do not determine it even when all of them are fixed.
type foldSeg struct {
	text     string
	optional bool
	guards   []guardRef
	opaque   bool
}

// foldVar is one tracked variable's accumulated value: the seed the expression
// walk already emits (never re-emitted here) plus the appends applied to it.
type foldVar struct {
	line int
	// col is the seed expression's column, carried for the same reason line is:
	// a fold world and the expression walk's own candidate both anchor at the
	// seed, so they must agree on BOTH coordinates or a consumer deduping by
	// position stops recognising them as the same statement (#44).
	col      int
	seed     string
	segs     []foldSeg
	appended bool
}

// foldScope is one block's fold state.
type foldScope struct {
	vars map[string]*foldVar
	// unfoldable: every name this scope must never start tracking, mapped to
	// the reason — a dereference write in the block (#74), a called named
	// closure (#72), an address escaping at a provably-run position (#310).
	// Computed once at scope entry: each of those spans statements no
	// per-statement pass can see together.
	unfoldable map[string]unread
	// arrays: every name this scope declares as `var x [N]T` with N positive,
	// the one range source whose non-emptiness a parse-only pass can prove
	// through a name (#314, untrack.go). Same reason it is computed here: the
	// declaration and the `for … = range x` clause are different statements.
	arrays map[string]bool
	// closureEscapes: for a name a closure appends to, the spelling of the call
	// that closure escaped into (#337, invoked.go). NOT a refusal — the query
	// keeps folding, because a parse-only pass cannot tell a callee that invokes
	// f from one that stores it. Computed at scope entry for unfoldable's reason:
	// the binding, the escape and the use are three different statements.
	closureEscapes map[string]string
	// sites: where this scope's refusals are recorded (#311, sites.go). Nil in
	// the package's own fold tests, which drive foldBlock for its candidates
	// alone; siteSink.add is nil-safe for exactly that reason.
	sites *siteSink
}

func foldBlock(sig *ast.FuncType, body *ast.BlockStmt, fset *token.FileSet, sites *siteSink) []Candidate {
	sc := &foldScope{vars: map[string]*foldVar{}, unfoldable: unfoldable(body), arrays: fixedArrays(sig, body), closureEscapes: escapedClosureRisk(body), sites: sites}
	// Before the statement walk, because a builder is never tracked and so is
	// never reached by it (#311, builder.go).
	builderSites(sig, body, sites)
	foldStmts(body.List, sc, fset, guardCtx{})

	names := make([]string, 0, len(sc.vars))
	for name := range sc.vars {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []Candidate
	for _, name := range names {
		v := sc.vars[name]
		if !v.appended {
			continue
		}
		// #238: mark the base world when this variable has a complementary
		// pair, so a consumer can say WHY a correct-looking query fired.
		//
		// THE OCCURRENCE COUNT IS UNREACHABLE WHILE pairs IS TRUE, and that is
		// a proof rather than a failed search: a complementary pair requires a
		// non-empty append on BOTH sides, so full (base plus every optional
		// append) and each branch world (base plus one branch's appends) all
		// differ from base by at least one append. occurrences[base] is
		// therefore always 1 here, no mutation of this term changes any output,
		// and it is an equivalent mutant — said plainly because a guard that
		// looks covered and is not is worse than one whose limit is written
		// down.
		//
		// It stays because that proof rests on what a PAIR is, not on what this
		// function does: complementaryCandidates skipping opaque segs, or a
		// future pair whose branch appends can render empty, would make base
		// coincide with a reachable world and the note would then be attached
		// to a world some path does produce — the one error this whole feature
		// must not make.
		worlds := foldWorlds(v)
		base := foldText(v, nil, false)
		pairs := len(complementaryCandidates(v.segs)) > 0
		occurrences := map[string]int{}
		for _, text := range worlds {
			occurrences[text]++
		}
		emitted := map[string]bool{}
		for _, text := range worlds {
			if text == v.seed || emitted[text] {
				continue
			}
			emitted[text] = true
			out = append(out, Candidate{
				Text:               text,
				Line:               v.line,
				Col:                v.col,
				InfeasibleBaseRisk: pairs && text == base && occurrences[text] == 1,
				ClosureEscapeRisk:  sc.closureEscapes[name],
			})
		}
	}
	return out
}

// guardCtx is the branch context of the statements being folded: optional
// reports whether an enclosing if-without-else branch guards them (their
// appends extend full only, not base), guards is the conjunction of
// conditions that fires them, outermost first, and opaque reports that at
// least one of those conditions is not a usable proposition.
//
// shadow reports that these statements are the body of an IIFE being folded
// INLINE into this scope. It is set at that one recursion step and carried on
// from there by nest (`child := g`), so it holds for every statement inside the
// body at any nesting depth — which is exactly what foldAssign needs: a `:=`
// binds in a block of the CLOSURE however deep it sits, so the outer variable
// of that name is provably never written and the statement is SKIPPED rather
// than untracked. Untracking it deletes a variable nobody wrote, and the
// post-closure appends with it (iifeModellable states the invariant).
type guardCtx struct {
	optional bool
	guards   []guardRef
	opaque   bool
	shadow   bool
}

// nest returns the context inside an if-without-else branch whose condition is
// cond, or an opaque one when cond cannot be read as a proposition. hasInit
// marks an `if` with an Init statement: it may BIND the name the condition
// tests (`if _, ok := m[k]; ok`), so two such ifs testing `ok` test two
// different values and neither can name a pair.
func (g guardCtx) nest(cond ast.Expr, hasInit bool) guardCtx {
	child := g
	child.optional = true
	path, negated, ok := "", false, false
	if !hasInit {
		path, negated, ok = foldGuard(cond)
	}
	if !ok {
		child.opaque = true
		return child
	}
	ref := guardRef{path: path, negated: negated}
	child.guards = append(append(make([]guardRef, 0, len(g.guards)+1), g.guards...), ref)
	return child
}

// foldStmts folds a straight-line statement list into sc, under the branch
// context g. Only assignments are folded; every other statement (a bare { }
// block, for/switch/…) untracks any tracked variable it assigns. A bare block is
// deliberately not recursed: a `q := …` inside it may shadow an outer q, and
// parse-only cannot tell a shadow from a reassignment, so folding it would
// fabricate a value no real path holds — untrack is sound.
// jumpedOver returns the indexes of statements a FORWARD goto in this list
// skips (#73).
//
// The fold walks a statement list linearly, so a forward jump's skipped
// statements were folded in as though control fell through and the jumped-over
// path had no world at all. That is the silent direction: when the skipped
// append supplies the ORDER BY, the goto path is an unordered locking SELECT
// and the only world emitted is the ordered fall-through one.
//
// A forward goto is structurally an OPTIONAL append — the statements between
// the jump and its label run on one path and not the other — and the fold
// already models optional appends, always emitting the world without them. So
// this reuses that rather than adding a second mechanism to keep in step.
//
// Marked opaque, not guarded: the condition that reaches the goto is not
// carried here, so these appends must not be paired with any `if` condition as
// a complementary guard. Opaque means "optional, and its guards do not
// determine it", which is exactly true.
//
// BACKWARD jumps are not modelled and not attempted. A backward goto is a loop,
// and the appends between the label and the branch repeat an unknown number of
// times; the fold has no representation for that.
//
// The `target > i` test is DEFENSIVE, not load-bearing, and that is measured
// rather than assumed: mutating it away, and even marking the whole backward
// span optional, changes no output. The reason sits upstream — a labeled
// statement reaches foldStmts' default case and untracks the variable before
// this runs. The guard stays because relying on that coincidence would be a
// worse comment than this one, but nothing here is pinned by a test and the
// test that looks like it pins it says so too.
func jumpedOver(stmts []ast.Stmt) map[int]bool {
	labelAt := map[string]int{}
	for i, st := range stmts {
		if ls, ok := st.(*ast.LabeledStmt); ok {
			labelAt[ls.Label.Name] = i
		}
	}
	skipped := map[int]bool{}
	for i, st := range stmts {
		ast.Inspect(st, func(n ast.Node) bool {
			br, ok := n.(*ast.BranchStmt)
			if !ok || br.Tok != token.GOTO || br.Label == nil {
				return true
			}
			if target, ok := labelAt[br.Label.Name]; ok && target > i {
				for j := i + 1; j < target; j++ {
					skipped[j] = true
				}
			}
			return true
		})
	}
	return skipped
}

func foldStmts(stmts []ast.Stmt, sc *foldScope, fset *token.FileSet, g guardCtx) {
	skipped := jumpedOver(stmts)
	for idx, st := range stmts {
		stmtCtx := g
		if skipped[idx] {
			stmtCtx.optional = true
			stmtCtx.opaque = true
		}
		switch s := st.(type) {
		case *ast.AssignStmt:
			foldAssign(s, sc, fset, stmtCtx)
		case *ast.IfStmt:
			// iifeModellable must stay in step with this whole case: the Init
			// untrack directly below and the Else untrack in the else-branch
			// are each unbounded past a *ast.FuncLit, so reached from an
			// inlined IIFE body they delete an OUTER variable by bare name.
			// iifeModellable's allowlist (Init == nil, Else == nil) is what
			// keeps them unreachable from there.
			// A LITERAL INVOKED IN THE HEADER runs unconditionally too, and
			// arrives through Cond or Init, neither of which any
			// write-detection path inspects: this arm reads Cond for guards
			// only, and untrackAssigned stops at *ast.FuncLit in both. Same
			// partial read as a disqualified IIFE, reached by another route
			// (locking.go's item 4, #311). recordHeaderLiterals answers every
			// statement that has a header, this one included, because splitting
			// the question by syntax form is how the class got here.
			recordHeaderLiterals(s, sc)
			if s.Init != nil {
				untrackAssigned(s.Init, sc)
			}
			if s.Else == nil {
				// Optional branch: its appends extend full only (base skips
				// them), and they fire on this branch's condition CONJOINED with
				// every enclosing one — a nested append is reachable only where
				// its parent branch is.
				foldStmts(s.Body.List, sc, fset, g.nest(s.Cond, s.Init != nil))
			} else {
				// Mandatory choice (both branches append): not forked — untrack
				// any tracked variable either branch assigns (spec §4.2).
				untrackAssigned(s, sc)
			}
		default:
			if body, ok := iifeBody(st); ok {
				if iifeModellable(body.List) {
					// Same guard context: the literal runs unconditionally where
					// it sits, so its appends inherit exactly the enclosing
					// branch conditions and an `if` inside it stays optional for
					// free. shadow is set because a `:=` in a func-literal body
					// binds in the closure's own scope (see foldAssign).
					// iifeModellable gates this: a body that could touch a
					// variable of THIS scope in a way the walk cannot model is
					// left opaque instead of partially inlined, so it cannot
					// leak an untrack into the ENCLOSING scope that the pre-#72
					// walk — sealed at the *ast.FuncLit boundary — never
					// reached (#72 review, round 2).
					child := g
					child.shadow = true
					foldStmts(body.List, sc, fset, child)
					continue
				}
				// A DISQUALIFIED IIFE IS NOT A SILENCE, and that is why it is
				// recorded rather than untracked. It runs unconditionally right
				// where it sits, so its appends are real; they are dropped while
				// the variable outside stays tracked, so the fold emits a world
				// built from part of the query and the rule judges that
				// (locking.go's false-positive item 3). An operator auditing a
				// clean run on such a file has no other signal —
				// untrackAssigned, below, stops at *ast.FuncLit and sees nothing
				// in here at all (#311).
				recordUnreadAppends(body, sc, reasonDisqualifiedIIFE)
			}
			// A `for` header, a `range` source, a `switch` tag or a `select`'s
			// channel operands can each invoke a literal that appends, and each
			// is evaluated on reaching the statement while untrackAssigned stops
			// at *ast.FuncLit and sees none of it (#311).
			recordHeaderLiterals(st, sc)
			untrackAssigned(st, sc)
		}
	}
}

func foldAssign(s *ast.AssignStmt, sc *foldScope, fset *token.FileSet, g guardCtx) {
	if g.shadow && s.Tok != token.ADD_ASSIGN {
		// INSIDE AN INLINED IIFE BODY NOTHING MAY UNTRACK AN OUTER VARIABLE.
		// iifeModellable admits exactly three assignment shapes here — a `:=`,
		// an `=` naming no variable (`_ = q`, `o.f = q`), and a single-target
		// `+=` carrying real literal text — and the first two provably cannot
		// write a variable of the ENCLOSING scope: a `:=` binds in a block of
		// the closure, and an `=` that names no variable writes none. Both are
		// SKIPPED, neither tracked nor untracked, which is the only treatment
		// that leaves the outer variable as the closure really left it. Anything
		// that MIGHT write one never reaches this walk — it disqualified the
		// whole body and the literal was left opaque instead.
		//
		// Every remaining `delete` below is therefore unreachable while
		// g.shadow holds: the multi-LHS loop needs an arity iifeModellable does
		// not admit, and the two in the DEFINE/ASSIGN arm sit past this return.
		// No input reaches them, well-typed or not.
		//
		// Untracking here was the design through three review rounds and it
		// deleted variables the closure provably never wrote, dropping every
		// later append and reporting NOTHING where base reported an unordered
		// locking SELECT (#72 round 4; measured 1 → 0 on five shapes).
		return
	}
	// Only a single-ident LHS with a single RHS is tracked; anything else
	// untracks every ident it assigns.
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		for _, lhs := range s.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
				delete(sc.vars, id.Name)
			}
		}
		return
	}
	// ast.Unparen: `(q) += …` is valid Go and survives gofmt, and matching a
	// bare *ast.Ident here left the variable TRACKED while the append vanished —
	// a fabricated world, reachable with no closure involved (#72, spec §4.2).
	// A selector or index target genuinely cannot be a tracked name, so the
	// early return below is correct once the parens are off.
	id, ok := ast.Unparen(s.Lhs[0]).(*ast.Ident)
	if !ok {
		return // selector/index target: cannot be a tracked variable
	}
	name := id.Name
	switch s.Tok {
	case token.DEFINE, token.ASSIGN:
		if g.optional {
			// A `:=`/`=` inside a branch may be a shadow; parse-only cannot tell
			// a shadow from a reassignment, so abandon tracking (sound). This
			// reads on g.optional ALONE: inside an inlined func-literal body the
			// guard at the top of this function has already returned, and an
			// untrack there would delete a variable of the enclosing scope
			// rather than one this branch could have written.
			delete(sc.vars, name)
			return
		}
		text, ok := reassemble(s.Rhs[0])
		if !ok {
			delete(sc.vars, name)
			return
		}
		if u, refused := sc.unfoldable[name]; refused {
			// A write to it can be invisible to this pass: through a
			// dereference in the block (#74), through a closure (#72), or on
			// the far side of an escaping address (#310). Never start tracking
			// it: emitting nothing beats emitting a world assembled from only
			// the writes we happened to see.
			//
			// The refusal is RECORDED, with the seed text just reassembled and
			// anchored at the invisible write rather than here (#311). Not
			// here, because the expression walk emits THIS line's literal as a
			// candidate the rule really does analyse — a site on it would say
			// "not analysed" about a line `formwork check` can fail on, which
			// is the false claim this whole change closes.
			sc.sites.add(u.pos, u.reason, text)
			delete(sc.vars, name)
			return
		}
		sc.vars[name] = &foldVar{line: fset.Position(s.Rhs[0].Pos()).Line, col: fset.Position(s.Rhs[0].Pos()).Column, seed: text}
	case token.ADD_ASSIGN:
		v, tracked := sc.vars[name]
		if !tracked {
			return // append to an untracked variable: unmodeled (miss)
		}
		app, _ := reassembleOperand(s.Rhs[0])
		v.appended = true
		v.segs = append(v.segs, foldSeg{
			text:     app,
			optional: g.optional,
			guards:   g.guards,
			opaque:   g.opaque,
		})
	default:
		delete(sc.vars, name) // -=, *=, … on a string var: abandon
	}
}

// foldGuard reads an exact if-condition as a proposition about one stored
// value: a path — an ident or a chain of field selections off one (`a`,
// `o.Ordered`) — optionally negated, parentheses stripped, so `a`, `(a)`, `!a`
// and `!(a)` share one path. Only a stored value names a pair: a CALL may
// return different values on its two evaluations, so `if opts.Order()` /
// `if !opts.Order()` names nothing, and a compound condition is not tracked at
// all. Anything else reports ok == false, which leaves the variable on the
// bounded pair. Complementary forms this does not equate (`x == 1` vs
// `x != 1`) are the same disclosed miss.
//
// Nothing here decides whether the two reads of a path really SEE one value.
// That is the proof four review rounds could not carry parse-only (§10), and
// this model no longer asks for it: a pair only ever ADDS branch worlds, so a
// wrongly paired guard costs a redundant candidate — never a deleted one.
// TestFromGoReassembledPairEligibilityBoundary pins what this does exclude,
// because exclusion is the direction that can lose a hazard.
func foldGuard(cond ast.Expr) (path string, negated bool, ok bool) {
	e := ast.Unparen(cond)
	for {
		u, isUnary := e.(*ast.UnaryExpr)
		if !isUnary || u.Op != token.NOT {
			break
		}
		negated = !negated
		e = ast.Unparen(u.X)
	}
	path, ok = guardPath(e)
	return path, negated, ok
}

// guardPath renders a stored-value path: `a`, `o.Ordered`, `o.opts.Ordered`.
// Anything with a call, index, or dereference in it is not one.
func guardPath(e ast.Expr) (string, bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x.Name, true
	case *ast.SelectorExpr:
		base, ok := guardPath(x.X)
		if !ok {
			return "", false
		}
		return base + "." + x.Sel.Name, true
	}
	return "", false
}
