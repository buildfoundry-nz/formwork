package sqlextract_test

import "testing"

// Tests for inline folding of an immediately-invoked function literal (#72).
//
// `func(){ … }()` provably runs, exactly once, where it is written, so its
// appends interleave with the surrounding ones and the reassembled value is the
// real one. Before #72 the enclosing walk halted at the literal, dropping its
// appends WHILE LEAVING THE VARIABLE TRACKED, so the fold emitted a value no
// execution path produces.
//
// The guards below are as load-bearing as the fixes. Spec §10 records a design
// that fixed the fabrication by REFUSING any closure-written variable; it was
// rejected because it deleted the true positives those guards pin. Split from
// fold_test.go under the 750-line vendor cap.

const iifeSeed = "SELECT id FROM t WHERE s = 'x'"

func iifeFolds(t *testing.T, body string) []string {
	t.Helper()
	src := "package db\n\nfunc exec(s string) {}\nfunc run(f func()) {}\n\n" +
		"func load(b bool) string {\n" + body + "\treturn q\n}\n"
	return foldOnly(foldTexts(t, src), iifeSeed)
}

func TestFromGoReassembledIIFEAppendIsFoldedInOrder(t *testing.T) {
	// #72's noisy direction: the ORDER BY is inside the IIFE, so the real value
	// is ordered. The fold must reconstruct it, not emit the value that drops it.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tq += \" AND y = 1\"\n" +
		"\tfunc() { q += \" ORDER BY id\" }()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " AND y = 1 ORDER BY id FOR UPDATE"
	bad := iifeSeed + " AND y = 1 FOR UPDATE"
	folds := iifeFolds(t, body)
	if !hasFoldText(folds, want) {
		t.Fatalf("an IIFE's append must be folded in order: want %q in %q", want, folds)
	}
	if hasFoldText(folds, bad) {
		t.Fatalf("the world that drops the IIFE's append must not be emitted: %q", folds)
	}
}

func TestFromGoReassembledIIFELockIsFolded(t *testing.T) {
	// #72's SILENT direction, the one the issue exists for. The FOR UPDATE is
	// inside the IIFE, so the real value is an unordered locking SELECT. The
	// fold must produce it — this is the world the gate needs in order to fire.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tq += \" AND y = 1\"\n" +
		"\tfunc() { q += \" FOR UPDATE\" }()\n"
	want := iifeSeed + " AND y = 1 FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("an IIFE's lock must be folded in: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledNonIIFEClosureKeepsFolding(t *testing.T) {
	// LOAD-BEARING GUARD, green throughout. For a closure that is conditionally
	// called, never called, or created after the value is used, the value
	// assembled from the appends OUTSIDE it is a real execution path — the
	// not-called path — so emitting it is CORRECT and a finding on it is a true
	// positive. Spec §10 records a rejected design that deleted exactly these.
	//
	// If you are widening the IIFE rule and these go red, you are re-making that
	// mistake. Read §4.2 before changing them.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"conditionally called", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tadd := func() { q += \" ORDER BY id\" }\n\tif b {\n\t\tadd()\n\t}\n" +
			"\tq += \" FOR UPDATE\"\n"},
		{"never called", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tadd := func() { q += \" ORDER BY id\" }\n\t_ = add\n" +
			"\tq += \" FOR UPDATE\"\n"},
		{"created after the use", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tq += \" FOR UPDATE\"\n\texec(q)\n" +
			"\tadd := func() { q += \" ORDER BY id\" }\n\t_ = add\n"},
		{"passed to a helper", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\trun(func() { q += \" ORDER BY id\" })\n" +
			"\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := iifeFolds(t, tc.body); !hasFoldText(folds, want) {
				t.Fatalf("a non-IIFE closure must not stop the fold: want %q in %q", want, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEShadowKeepsTheOuterVariable(t *testing.T) {
	// INVERTED in round 4, from an assertion that both shapes UNTRACK. Untracking
	// reads as the safe direction and is not: it deletes a variable the closure
	// provably never wrote, dropping the post-closure append with it, so the
	// unordered locking SELECT this seed becomes reported NOTHING where base
	// reported it — measured 1 → 0 at the gate on both shapes.
	//
	// `:=` inside a func-literal body binds in the CLOSURE's own block, so the
	// outer variable of that name cannot be its target: the statement is skipped
	// (neither tracked nor untracked) and the outer variable survives intact.
	//
	// `=` with a non-blank target MIGHT write the outer variable, and parse-only
	// cannot tell whether it does, so it disqualifies the whole body and the
	// literal is left opaque — base behaviour, byte for byte. Two mechanisms,
	// one observable: the outer variable, and its post-closure append, survive.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"define", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() { q := \"SELECT 2\"; _ = q }()\n\tq += \" FOR UPDATE\"\n"},
		{"assign", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() { q = \"SELECT 2\" }()\n\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := iifeFolds(t, tc.body); !hasFoldText(folds, want) {
				t.Fatalf("a write inside a func literal must not delete the outer variable: want %q in %q", want, folds)
			}
		})
	}
}

func TestFromGoReassembledParameterisedIIFEIsNotModelled(t *testing.T) {
	// A parameter or a named result can SHADOW the name the body writes, and
	// parse-only cannot tell which. Such a literal is not modelled inline; the
	// enclosing walk keeps folding, exactly as for a non-IIFE closure.
	//
	// The body appends rather than merely reading the parameter (`_ = q` was a
	// no-op that left this test green whether or not the Params exclusion
	// existed — deleting it and re-running showed the assertion still held,
	// since a no-op statement disturbs nothing either way). `q += …` here
	// targets the PARAMETER's local copy; folding it into the outer `q` would
	// fabricate a value the call — string parameters are passed by value —
	// never produces, so `bad` pins the exclusion where `_ = q` could not.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc(q string) { q += \" ORDER BY id\" }(q)\n\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " FOR UPDATE"
	bad := iifeSeed + " ORDER BY id FOR UPDATE"
	folds := iifeFolds(t, body)
	if !hasFoldText(folds, want) {
		t.Fatalf("a parameterised literal must not disturb the outer fold: want %q in %q", want, folds)
	}
	if hasFoldText(folds, bad) {
		t.Fatalf("a parameterised literal's append must not be folded into the outer variable it shadows: %q in %q", bad, folds)
	}
}

func TestFromGoReassembledIIFEKeepsGuardSemantics(t *testing.T) {
	// The IIFE body is recursed under the SAME guard context, so an `if` inside
	// it yields an OPTIONAL append with no extra machinery — `base` (order
	// skipped, lock applied) is a real path and a genuine hazard.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() { if b {\n\t\tq += \" ORDER BY id\"\n\t} }()\n" +
		"\tq += \" FOR UPDATE\"\n"
	folds := iifeFolds(t, body)
	for _, want := range []string{
		iifeSeed + " ORDER BY id FOR UPDATE", // the b == true world
		iifeSeed + " FOR UPDATE",             // the b == false world: the hazard
	} {
		if !hasFoldText(folds, want) {
			t.Errorf("an if inside an IIFE must stay optional: want %q in %q", want, folds)
		}
	}
}

func TestFromGoReassembledIIFEOwnQueryStillFolds(t *testing.T) {
	// An IIFE body is now walked twice — inline here, and as its own scope root.
	// The two track disjoint variables, so a query the IIFE seeds AND appends
	// itself belongs to the root walk alone and is emitted exactly once.
	src := "package db\n\nfunc outer() {\n" +
		"\tfunc() {\n\t\tr := `SELECT id FROM u`\n\t\tr += \" FOR UPDATE\"\n\t\t_ = r\n\t}()\n}\n"
	want := "SELECT id FROM u FOR UPDATE"
	texts := foldTexts(t, src)
	n := 0
	for _, got := range texts {
		if got == want {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("an IIFE's own query must be emitted exactly once, got %d in %q", n, texts)
	}
}

// The four tests below close a second review round on #72 (base 19a3ed7 vs
// the first IIFE-inlining commit c975164). Both defects share one shape: the
// inline walk treated a construct it does not model as if it were not there,
// which is unsound in a way plain untracking is not — a `return` makes the
// STATEMENT AFTER IT conditional in a way the linear walk cannot see, and an
// unmodelled loop/switch reaches the SHARED scope in a way it never could
// pre-#72 (untrackAssigned stops at a *ast.FuncLit, so nothing inside a
// closure could ever untrack an outer variable). The fix is all-or-nothing:
// an IIFE is folded inline only when iifeModellable(body) holds for every
// statement, recursively; otherwise the whole literal is left opaque, exactly
// as every closure was before this task.

func TestFromGoReassembledIIFEEarlyReturnDoesNotFabricateTheAppend(t *testing.T) {
	// CRITICAL, found on review. Both `!b` and `b` are real paths: the early
	// return means the ORDER BY only applies when b is true, so both
	// "…FOR UPDATE" (b==false) and "…ORDER BY id FOR UPDATE" (b==true) are
	// real values. The first IIFE-inlining commit folded the append as if it
	// were unconditional (a linear walk has no notion of "this statement is
	// unreachable when a preceding sibling returned"), so it emitted ONLY the
	// ordered text — silencing the real, unordered b==false path, which is
	// the deadlock hazard sql/locking-select-order exists to catch.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tif !b {\n\t\t\treturn\n\t\t}\n\t\tq += \" ORDER BY id\"\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " FOR UPDATE"
	bad := iifeSeed + " ORDER BY id FOR UPDATE"
	folds := iifeFolds(t, body)
	if !hasFoldText(folds, want) {
		t.Fatalf("a body containing a return must be left opaque, keeping the outer append alone: want %q in %q", want, folds)
	}
	if hasFoldText(folds, bad) {
		t.Fatalf("a return-guarded append must not be folded as if it were unconditional: %q", folds)
	}
}

func TestFromGoReassembledIIFEUnmodelledConstructDoesNotUntrackTheOuterVar(t *testing.T) {
	// IMPORTANT, found on review. Before this task the whole closure was
	// opaque to TRACKING, so nothing inside it could ever untrack the outer
	// q. Once the body is walked inline, a construct this pass does not model
	// (a for/range, a switch, …) used to fall to the same untrackAssigned an
	// ordinary unmodelled statement gets — but now applied INSIDE the shared
	// scope, so it reached the outer q and deleted it, losing the outer
	// append entirely even though the empty-range / no-matching-case path
	// makes that append's text a real value.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"for range", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\txs := []string{\"a\"}\n" +
			"\tfunc() {\n\t\tfor _, x := range xs {\n\t\t\tq += x\n\t\t}\n\t}()\n" +
			"\tq += \" FOR UPDATE\"\n"},
		{"switch", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() {\n\t\tswitch {\n\t\tcase b:\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n\t}()\n" +
			"\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := iifeFolds(t, tc.body); !hasFoldText(folds, want) {
				t.Fatalf("an unmodelled construct inside an IIFE must not untrack the outer variable: want %q in %q", want, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFENestedLiteralReturnDoesNotDisqualifyTheBody(t *testing.T) {
	// GUARD. A return belongs to the func literal it is written in. `add` is
	// assigned a literal and never called here, so it is not itself an IIFE —
	// foldAssign already untracks `add`'s own name (a func literal is not a
	// reassemblable value) without ever looking inside its body — so the
	// return inside IT must not disqualify the OUTER IIFE's body, which is
	// otherwise fully modellable, from being folded.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tadd := func() { return }\n\t\t_ = add\n\t\tq += \" ORDER BY id\"\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " ORDER BY id FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("a return inside a further, non-invoked literal must not stop the outer IIFE from folding: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledIIFEMandatoryIfElseLeavesLiteralOpaque(t *testing.T) {
	// Found on re-review, round 2 — a defect in iifeModellable itself, not in
	// foldStmts. The predicate accepted an if WITH an else by recursing into
	// both branches, as if foldStmts folded it the same way it folds an
	// if-without-else. It does not: a mandatory if/else (both branches
	// append, a text choice) is untracked WHOLESALE via untrackAssigned(s, sc)
	// (spec §4.2) — never via foldStmts/foldAssign on either branch. Applied
	// inside an inlined body, that untrack is no longer sealed at any
	// *ast.FuncLit boundary: it deletes the outer q outright, discarding the
	// seed and the post-closure append that base's total opacity would have
	// left untouched.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tif b {\n\t\t\tq += \" ORDER BY id\"\n\t\t} else {\n\t\t\tq += \" AND z = 1\"\n\t\t}\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("an if/else (mandatory choice) inside an IIFE must leave the literal opaque, keeping the outer append alone: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledIIFEIfWithInitLeavesLiteralOpaque(t *testing.T) {
	// Found on re-review, round 3 — the SAME root cause as the if/else
	// finding (round 2), one level deeper. An if WITHOUT an else but WITH an
	// Init looked just as foldable — its Body is a single ordinary append —
	// but foldStmts untracks Init UNCONDITIONALLY, regardless of Else. A `:=`
	// in an if's Init opens the if statement's OWN scope
	// (`if q := "x"; q != "" { … }`'s q is a CLOSURE-LOCAL shadow of the
	// outer q), but untrackAssigned deletes by bare name, so it deleted the
	// OUTER q outright.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tif q := \"x\"; q != \"\" {\n\t\t\t_ = q\n\t\t}\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("an if with an Init inside an IIFE must leave the literal opaque, keeping the outer append alone: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledIIFEPlainIfStillInlines(t *testing.T) {
	// GUARD, so the Init/Else narrowing above does not silently disable the
	// ordinary case the whole feature exists for. An if with NEITHER an Init
	// NOR an Else is exactly the third of iifeModellable's three accepted
	// shapes, and must still be folded inline.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tif b {\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " ORDER BY id FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("an ordinary if without Init or Else inside an IIFE must still be folded inline: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledDeferAndGoLiteralsAreNotIIFEs(t *testing.T) {
	// MINOR, open since round 1: nothing pinned this. `defer func(){…}()` and
	// `go func(){…}()` call a func literal with the same shape an IIFE has,
	// but neither runs where it is written — a defer runs at return, a go
	// statement runs concurrently, whenever the scheduler gets to it.
	// iifeBody's *ast.ExprStmt assertion already excludes both (a
	// *ast.DeferStmt/*ast.GoStmt is its own statement type, never an
	// *ast.ExprStmt), so this pins that exclusion with a test.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"defer", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tdefer func() { q += \" ORDER BY id\" }()\n" +
			"\tq += \" FOR UPDATE\"\n"},
		{"go", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tgo func() { q += \" ORDER BY id\" }()\n" +
			"\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := iifeFolds(t, tc.body); !hasFoldText(folds, want) {
				t.Fatalf("a defer/go literal must not be treated as an IIFE: want %q in %q", want, folds)
			}
		})
	}
}

// The four tests below close round 4 on #72. All three rounds before it chased
// one symptom — a statement inside an inlined body untracking the OUTER
// variable by bare name and silencing the gate — through the untrack sites
// foldStmts owns. This round closes the last channel, the one iifeModellable's
// allowlist deliberately admits: foldAssign's untrack of names on an
// assignment's own Lhs. Inside an inlined body that untrack is not conservative
// but wrong for a `:=`, which binds anew in the closure's block and so provably
// never writes the outer variable of that name.
//
// The invariant, stated once: INSIDE AN INLINED IIFE BODY NO STATEMENT MAY
// UNTRACK AN OUTER VARIABLE. Every statement satisfies exactly one of three
// arms — it provably cannot write one (SKIP it); it is an append this pass can
// render as real text (FOLD it); or neither holds (DISQUALIFY the whole body,
// leaving the literal opaque and reproducing base exactly) — and an untrack is
// never among them.

func TestFromGoReassembledIIFEClosureLocalDefineKeepsTheOuterVariable(t *testing.T) {
	// The two shapes nothing pinned when round 3 shipped, each measured 1 → 0 at
	// the gate. A `:=` is a closure-local binding however many names it binds
	// and however deep in the body it sits — Go's short variable declaration
	// only ever declares (or redeclares) in the block it is written in, and
	// every block here is inside the literal — so neither can be the outer q.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"multi-name define", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() { a, q := \"1\", \"2\"; _, _ = a, q }()\n\tq += \" FOR UPDATE\"\n"},
		{"define inside an if", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() {\n\t\tif b {\n\t\t\tq := \"z\"\n\t\t\t_ = q\n\t\t}\n\t}()\n" +
			"\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := iifeFolds(t, tc.body); !hasFoldText(folds, want) {
				t.Fatalf("a closure-local define must not delete the outer variable: want %q in %q", want, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEOuterAssignLeavesLiteralOpaque(t *testing.T) {
	// The other side of the invariant. An `=` with a non-blank target inside the
	// body might write the outer q — `func(){ if b { q = "z" } }()` really does
	// when b — so the body cannot be inlined at all: neither skipping the write
	// (which would model a value the b branch never produces) nor honouring it
	// (parse-only cannot tell an outer write from a shadow) is sound. The whole
	// literal is left opaque, which is base behaviour byte for byte.
	//
	// The second subtest is what makes the opacity OBSERVABLE: with the body
	// opaque its trailing append is dropped, so the ordered world is absent. That
	// is a deliberate precision loss — the real values are all ordered, so base's
	// finding here is a false positive this round declines to fix — taken because
	// base-equality is the floor a disqualified body must reproduce.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"assign inside an if", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() {\n\t\tif b {\n\t\t\tq = \"z\"\n\t\t}\n\t}()\n" +
			"\tq += \" FOR UPDATE\"\n"},
		{"assign then append", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() {\n\t\tif b {\n\t\t\tq = \"z\"\n\t\t}\n\t\tq += \" ORDER BY id\"\n\t}()\n" +
			"\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folds := iifeFolds(t, tc.body)
			if !hasFoldText(folds, want) {
				t.Fatalf("an outer assign inside an IIFE must leave the literal opaque, keeping the outer append: want %q in %q", want, folds)
			}
			if bad := iifeSeed + " ORDER BY id FOR UPDATE"; hasFoldText(folds, bad) {
				t.Fatalf("a disqualified body's appends must not be folded: %q in %q", bad, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEShadowedAppendLeavesLiteralOpaque(t *testing.T) {
	// The hazard skipping a `:=` opens, and the reason skipping it is not enough
	// on its own. `sc.vars` is keyed by BARE NAME with no scope stack, so once
	// `q := …` is skipped rather than untracked, the `q += …` after it appends
	// the CLOSURE-LOCAL query's text to the OUTER q — fabricating a value no path
	// produces, and an ordered one at that, which silences the outer unordered
	// lock (measured: base 1 finding, skip-without-this-guard 0).
	//
	// A name bound by `:=` anywhere in the body and appended to with `+=`
	// anywhere in it therefore disqualifies the whole body. The check is
	// deliberately order-blind and scope-blind: a shape it refuses that a real
	// scope analysis would allow costs only precision, and costs it in the
	// direction of base, which is the floor.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tq := `SELECT other FROM u`\n\t\tq += \" ORDER BY id\"\n\t\t_ = q\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " FOR UPDATE"
	bad := iifeSeed + " ORDER BY id FOR UPDATE"
	folds := iifeFolds(t, body)
	if !hasFoldText(folds, want) {
		t.Fatalf("a body that shadows then appends must be left opaque, keeping the outer append: want %q in %q", want, folds)
	}
	if hasFoldText(folds, bad) {
		t.Fatalf("a closure-local variable's append must not be folded into the outer one: %q in %q", bad, folds)
	}
}

func TestFromGoReassembledIIFEMalformedAppendLeavesLiteralOpaque(t *testing.T) {
	// The last statement that could still untrack the outer variable from inside
	// an inlined body, and the one that survived this round's first pass. `+=` is
	// the ONE token the skip guard lets through to the fold, and go/parser
	// accepts `a, q += "1", "2"` and `q += "1", "2"` — a type error, not a parse
	// error — which lands in foldAssign's multi-LHS/RHS arm and deletes every
	// ident on the Lhs, the outer q among them.
	//
	// "It would not compile" is not a reason a lockdown gate may untrack: this
	// pass never type-checks its input and runs over whatever bytes are in a
	// .go file. An admitted `+=` is therefore exactly the single-target,
	// single-value shape foldAssign folds; any other arity disqualifies the body,
	// which is what makes the guarantee "foldAssign performs NO delete at all
	// while g.shadow holds" true of every input rather than of well-typed ones.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"multiple targets", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() { a, q += \"1\", \"2\"; _, _ = a, q }()\n\tq += \" FOR UPDATE\"\n"},
		{"multiple values", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfunc() { q += \"1\", \"2\" }()\n\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := iifeFolds(t, tc.body); !hasFoldText(folds, want) {
				t.Fatalf("an append this pass cannot fold must not untrack the outer variable: want %q in %q", want, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEBareNonLiteralAppendLeavesLiteralOpaque(t *testing.T) {
	// Found by the base-equality differential, round 5, and it sits outside the
	// invariant's untrack arms — which is why four rounds missed it. An append
	// whose RHS carries NO real literal text anywhere (a call, a variable, an
	// index expression, two variables concatenated) renders as the bare
	// "fw_expr" placeholder, which the fold glues straight onto the accumulated
	// text: `…WHERE s = 'x'` + `fw_expr` is a token no query contains, so the
	// candidate fails to parse and is dropped SILENTLY. Base left the literal
	// opaque, dropped the append, and emitted a candidate that does parse, so
	// the gate reported the lock. Folding honestly here therefore trades a
	// reported deadlock hazard for nothing at all.
	//
	// Such a body is not inlined. That restores base exactly and costs no
	// coverage: the honest fold of it is unparseable, so inlining bought
	// nothing the gate could ever see.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body, bad string }{
		{"call", "\tbuildClause := func() string { return \"\" }\n" +
			"\tfunc() { q += buildClause() }()\n", iifeSeed + "fw_expr FOR UPDATE"},
		{"identifier", "\tclause := \"ORDER BY id\"\n" +
			"\tfunc() { q += clause }()\n", iifeSeed + "fw_expr FOR UPDATE"},
		{"index expression", "\tparts := []string{\"ORDER BY id\"}\n" +
			"\tfunc() { q += parts[0] }()\n", iifeSeed + "fw_expr FOR UPDATE"},
		{"two identifiers", "\tclause, col := \"ORDER BY\", \" id\"\n" +
			"\tfunc() { q += clause + col }()\n", iifeSeed + "fw_exprfw_expr FOR UPDATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" + tc.body + "\tq += \" FOR UPDATE\"\n"
			folds := iifeFolds(t, body)
			if !hasFoldText(folds, want) {
				t.Fatalf("an append with no literal text must leave the literal opaque, keeping the outer append: want %q in %q", want, folds)
			}
			if hasFoldText(folds, tc.bad) {
				t.Fatalf("a placeholder glued to the accumulated text must not be emitted: %q in %q", tc.bad, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEMixedAppendStillInlines(t *testing.T) {
	// GUARD, green before and after, so the narrowing above cannot widen into
	// the common shape. `" ORDER BY " + col` carries real literal text, so its
	// placeholder lands in a column position rather than glued to the preceding
	// token, and the folded text parses — which is the whole reason this append
	// is worth folding. Only an append with NO literal text anywhere is refused.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tcol := \"id\"\n" +
		"\tfunc() { q += \" ORDER BY \" + col }()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " ORDER BY fw_expr FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("a mixed literal/non-literal append must still be folded inline: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledIIFEPlaceholderOnlyAppendLeavesLiteralOpaque(t *testing.T) {
	// Found by the base-equality differential, round 6. The previous check asked
	// reassembleOperand whether the RHS contained a literal TOKEN — provenance,
	// which its own doc says is what that bool reports — and an empty literal or
	// a bare `%s` format is a token carrying nothing. Both render to the bare
	// `fw_expr` placeholder, byte for byte what `q += col` renders to, and reach
	// the same silence: glued onto the accumulated value it parses as nothing,
	// so the candidate is dropped and the lock base reported goes unreported.
	//
	// What the append RENDERS is therefore what decides it: there must be real
	// content before its first placeholder, not merely somewhere in it. Whitespace is
	// not content, and that is measured rather than assumed — ` fw_expr`,
	// `fw_expr `, ` fw_expr ` and "\tfw_expr" were each checked at the gate and
	// each parses as nothing, exactly like the bare placeholder. A separator
	// cannot give a placeholder a syntactic home.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, expr, bad string }{
		{"empty literal", "\"\" + col", iifeSeed + "fw_expr FOR UPDATE"},
		{"bare format verb", "fmt.Sprintf(\"%s\", col)", iifeSeed + "fw_expr FOR UPDATE"},
		{"leading space", "\" \" + col", iifeSeed + " fw_expr FOR UPDATE"},
		{"trailing space", "col + \" \"", iifeSeed + "fw_expr  FOR UPDATE"},
		{"tab", "\"\\t\" + col", iifeSeed + "\tfw_expr FOR UPDATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
				"\tcol := \"id\"\n" +
				"\tfunc() { q += " + tc.expr + " }()\n" +
				"\tq += \" FOR UPDATE\"\n"
			folds := iifeFolds(t, body)
			if !hasFoldText(folds, want) {
				t.Fatalf("an append rendering to nothing but a placeholder must leave the literal opaque: want %q in %q", want, folds)
			}
			if hasFoldText(folds, tc.bad) {
				t.Fatalf("a placeholder glued to the accumulated text must not be emitted: %q in %q", tc.bad, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFERenderedContentAppendStillInlines(t *testing.T) {
	// GUARD, green before and after: the narrowing above must not creep into an
	// append whose rendering DOES carry content once the placeholder is removed.
	// Both forms leave ` ORDER BY ` / `ORDER BY `, which is where the placeholder
	// gets a column position instead of being glued to the preceding token, and
	// both parse — which is the whole reason they are worth folding.
	for _, tc := range []struct{ name, expr, want string }{
		{"concatenation", "\" ORDER BY \" + col", iifeSeed + " ORDER BY fw_expr FOR UPDATE"},
		{"format string", "fmt.Sprintf(\"ORDER BY %s\", col)", iifeSeed + "ORDER BY fw_expr FOR UPDATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
				"\tcol := \"id\"\n" +
				"\tfunc() { q += " + tc.expr + " }()\n" +
				"\tq += \" FOR UPDATE\"\n"
			if folds := iifeFolds(t, body); !hasFoldText(folds, tc.want) {
				t.Fatalf("an append with real rendered content must still be folded inline: want %q in %q", tc.want, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFELeadingPlaceholderAppendLeavesLiteralOpaque(t *testing.T) {
	// The last two shapes the base-equality differential still lost, at both
	// placements it measured them. Each renders to text that STARTS with the
	// placeholder — `fw_expr ASC`, `fw_expr ORDER BY id` — and the fold
	// concatenates with no separator, so the placeholder is glued to the last
	// token of the accumulated value: `…s = 'x'` + `fw_expr` is one token no
	// query contains. Content AFTER the placeholder cannot rescue that; the
	// parse has already failed to the left of it, which is why "content
	// somewhere" admitted these and "content before the first placeholder"
	// does not.
	want := iifeSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, call, bad string }{
		{"concatenation", "func() { q += clause + \" ASC\" }()",
			iifeSeed + "fw_expr ASC FOR UPDATE"},
		{"concatenation, nested", "func() { func() { q += clause + \" ASC\" }() }()",
			iifeSeed + "fw_expr ASC FOR UPDATE"},
		{"leading format verb", "func() { q += fmt.Sprintf(\"%s ORDER BY id\", clause) }()",
			iifeSeed + "fw_expr ORDER BY id FOR UPDATE"},
		{"leading format verb, nested", "func() { func() { q += fmt.Sprintf(\"%s ORDER BY id\", clause) }() }()",
			iifeSeed + "fw_expr ORDER BY id FOR UPDATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
				"\tclause := \"id\"\n\t" + tc.call + "\n\tq += \" FOR UPDATE\"\n"
			folds := iifeFolds(t, body)
			if !hasFoldText(folds, want) {
				t.Fatalf("an append whose rendering starts with a placeholder must leave the literal opaque: want %q in %q", want, folds)
			}
			if hasFoldText(folds, tc.bad) {
				t.Fatalf("a placeholder glued to the accumulated text must not be emitted: %q in %q", tc.bad, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEContentBeforePlaceholderStillInlines(t *testing.T) {
	// GUARD, green before and after, at both placements: the position rule must
	// not creep into an append whose literal text comes FIRST. There the
	// placeholder is separated from the accumulated value by the append's own
	// text and lands in a column position, so the folded value parses — which is
	// the whole reason these are worth folding, and how a lock or an order added
	// inside a closure is seen at all.
	for _, tc := range []struct{ name, call, want string }{
		{"concatenation", "func() { q += \" ORDER BY \" + clause }()",
			iifeSeed + " ORDER BY fw_expr FOR UPDATE"},
		{"concatenation, nested", "func() { func() { q += \" ORDER BY \" + clause }() }()",
			iifeSeed + " ORDER BY fw_expr FOR UPDATE"},
		{"format string", "func() { q += fmt.Sprintf(\"ORDER BY %s\", clause) }()",
			iifeSeed + "ORDER BY fw_expr FOR UPDATE"},
		{"format string, nested", "func() { func() { q += fmt.Sprintf(\"ORDER BY %s\", clause) }() }()",
			iifeSeed + "ORDER BY fw_expr FOR UPDATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
				"\tclause := \"id\"\n\t" + tc.call + "\n\tq += \" FOR UPDATE\"\n"
			if folds := iifeFolds(t, body); !hasFoldText(folds, tc.want) {
				t.Fatalf("an append with content before its placeholder must still be folded inline: want %q in %q", tc.want, folds)
			}
		})
	}
}

func TestFromGoReassembledIIFEUnshadowedLocalStillInlines(t *testing.T) {
	// GUARD, so the shadow rules above do not silently disable the feature. A
	// `:=` binding a name the body never appends to is skipped and nothing else
	// changes: the body is still inlined and its append still folds. Without
	// this, "disqualify on any define" would pass every other test in this file
	// while quietly returning the noisy direction of #72 to base.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tc := \"id\"\n\t\t_ = c\n\t\tq += \" ORDER BY id\"\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " ORDER BY id FOR UPDATE"
	if folds := iifeFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("a local define that shadows nothing must not stop the body being inlined: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledResultsOnlyIIFEIsNotModelled(t *testing.T) {
	// MINOR, open since round 1: nothing pinned this. A literal with a NAMED
	// result can shadow the outer name via a naked return assigning to it,
	// and parse-only cannot tell — iifeBody excludes any literal with a
	// non-empty Results list, named or not, symmetric with the Params
	// exclusion. (An unnamed result, as below, cannot shadow this way; it is
	// excluded anyway rather than carving out a narrower rule for one case.)
	//
	// No `return` in the body: with one, `iifeShapesAdmissible`'s default arm
	// disqualifies the body on its own (a return is never an admitted shape),
	// so the Results exclusion is never reached and this test cannot tell
	// whether it exists. `func() string { q += … }()` doesn't compile — a
	// result with no return — but this pass never type-checks its input, the
	// same reasoning already relied on for `a, q := "1","2"` elsewhere in this
	// file; it parses, and is what actually exercises the exclusion. `bad`
	// pins that: without it, this literal's append would fold into the outer
	// `q` exactly like an ordinary IIFE's.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() string { q += \" ORDER BY id\" }()\n" +
		"\tq += \" FOR UPDATE\"\n"
	want := iifeSeed + " FOR UPDATE"
	bad := iifeSeed + " ORDER BY id FOR UPDATE"
	folds := iifeFolds(t, body)
	if !hasFoldText(folds, want) {
		t.Fatalf("a results-only literal must not be modelled inline: want %q in %q", want, folds)
	}
	if hasFoldText(folds, bad) {
		t.Fatalf("a results-only literal's append must not be folded into the outer variable: %q in %q", bad, folds)
	}
}
