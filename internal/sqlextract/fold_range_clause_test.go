// fold_range_clause_test.go — #314.
//
// A `for … = range …` clause writes through RangeStmt.Key/Value, not through an
// AssignStmt, so untrackAssigned — which matched *ast.AssignStmt and *ast.FuncLit
// and nothing else — never saw it. The variable stayed tracked with the loop's
// overwrite dropped, and the fold emitted a value assembled from only the writes
// it happened to see: the #72/#73/#74 wrong-emit class at a shape none of them
// named.
//
// THE FIX IS NOT "UNTRACK EVERY RANGE CLAUSE", and that is the whole content of
// this file. #72's third comment re-adjudicated `for q = range m` as CORRECT and
// lists zero-iteration `for`/`range` as explicitly out of scope: over a map,
// slice, channel or iterator function the zero-iteration path is a REAL
// execution path, so the variable really does survive the loop and the emitted
// world really is produced. Untracking there deletes true positives — the
// measured cost that killed the first design of this fold (10 findings removed,
// 8 of them true positives).
//
// What genuinely fabricates is the residue that adjudication does not reach: a
// range over a source that is PROVABLY NON-EMPTY, where zero iterations is not
// available, so the loop certainly overwrites the variable and the emitted world
// is a value no path produces. That is decidable by SHAPE — no types — for a
// `var arr [N]T` with a positive N, a composite literal with an element, and an
// array literal whose length is positive however many elements are written.
//
// So the file has two halves and both are load-bearing: the fabrications that
// must stop, and the possibly-empty sources that must keep folding.
package sqlextract_test

import "testing"

// rangeSeed is the seed every case here composes on: a locking SELECT with no
// ORDER BY once " FOR UPDATE" is appended, i.e. the shape
// sql/locking-select-order fires on. Emitting it when no path produces it is a
// false deadlock finding; not emitting it when a path does is a silenced hazard.
const rangeSeed = "SELECT id FROM t WHERE s = 'x'"

func rangeSrc(body string) string {
	return "package db\n\nfunc q(m map[string]string) string {\n" +
		"\tq := \"" + rangeSeed + "\"\n" + body +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
}

// The issue's own reproduction. `var arr [2]string` iterates twice, so q after
// the loop is "" and the function returns " FOR UPDATE" — not a query at all.
func TestRangeOverAFixedLengthArrayDoesNotFabricateAWorld(t *testing.T) {
	src := rangeSrc("\tvar arr [2]string\n\tfor _, q = range arr {\n\t}\n")
	if worlds := foldOnly(foldTexts(t, src), rangeSeed); len(worlds) != 0 {
		t.Fatalf("a range over [2]string overwrites q twice, so the emitted world "+
			"is a query no path produces; got %q", worlds)
	}
}

// The same write with the source written inline. One element is enough: the
// loop runs, so the seed is gone.
func TestRangeOverANonEmptyCompositeLiteralDoesNotFabricateAWorld(t *testing.T) {
	src := rangeSrc("\tfor _, q = range []string{\"a\"} {\n\t}\n")
	if worlds := foldOnly(foldTexts(t, src), rangeSeed); len(worlds) != 0 {
		t.Fatalf("a range over a one-element slice literal overwrites q, so the "+
			"emitted world is a query no path produces; got %q", worlds)
	}
}

// An ARRAY literal is non-empty by its TYPE, not by its elements: `[2]string{}`
// writes no element and still iterates twice. Reading Elts alone would let this
// one through.
func TestRangeOverAFixedLengthArrayLiteralDoesNotFabricateAWorld(t *testing.T) {
	src := rangeSrc("\tfor _, q = range [2]string{} {\n\t}\n")
	if worlds := foldOnly(foldTexts(t, src), rangeSeed); len(worlds) != 0 {
		t.Fatalf("[2]string{} has length 2 whatever its elements, so the loop "+
			"overwrites q and the emitted world is unproducible; got %q", worlds)
	}
}

// `(q) = range …` is valid Go and survives gofmt. Spec §4.2 credits the
// ast.Unparen change in foldAssign with closing this shape; it did not — a
// RangeStmt never reaches foldAssign — so the parens have to be stripped here
// too. The source is a one-entry map literal, whose key type makes this
// well-typed as well as reachable.
func TestParenthesisedRangeTargetIsUntrackedToo(t *testing.T) {
	src := rangeSrc("\tfor (q) = range map[string]int{\"a\": 1} {\n\t}\n")
	if worlds := foldOnly(foldTexts(t, src), rangeSeed); len(worlds) != 0 {
		t.Fatalf("`for (q) = range` writes q exactly as `for q = range` does; "+
			"got %q", worlds)
	}
}

// A NAME can be bound to an array by three spellings and they are one shape.
// Reading only `var x [N]T` would close the issue's own reproduction and leave
// the other two fabricating — patching per syntax form is how this defect class
// got to a third issue. What makes a NAME provable is that its length lives in
// its TYPE: an array variable cannot be reassigned to a shorter array, so the
// binding decides the loop's trip count wherever the loop sits.
func TestRangeOverANameBoundToAnArrayDoesNotFabricateAWorld(t *testing.T) {
	cases := []struct{ name, decl, why string }{
		{"var-with-array-type", "\tvar arr [2]string\n",
			"the length is written in the declared type"},
		{"var-with-array-literal", "\tvar arr = [2]string{}\n",
			"no declared type, but the literal's type is the array's"},
		{"short-declaration", "\tarr := [2]string{}\n",
			"`:=` binds the same array type `var` does"},
		{"ellipsis-length", "\tarr := [...]string{\"a\", \"b\"}\n",
			"[...] takes its length from the elements: two of them, so two trips"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := rangeSrc(c.decl + "\t_ = arr\n\tfor _, q = range arr {\n\t}\n")
			if worlds := foldOnly(foldTexts(t, src), rangeSeed); len(worlds) != 0 {
				t.Fatalf("the loop certainly overwrites q (%s), so this world is a "+
					"query no path produces; got %q", c.why, worlds)
			}
		})
	}
}

// AN ARRAY NAME IS NOT ALWAYS BOUND BY A STATEMENT IN THE BLOCK. A parameter and
// a named result are declared in the function SIGNATURE; a variable captured
// from an enclosing function is declared in neither, and its only evidence
// inside this scope is an assignment to it. Each ranges over an array whose
// length is as fixed as any local's, and each fabricated while fixedArrays read
// only the block's own declarations.
func TestRangeOverAnArrayDeclaredOutsideTheBlockDoesNotFabricateAWorld(t *testing.T) {
	cases := []struct{ name, src, why string }{
		{"parameter",
			"package db\n\nfunc f(arr [2]string) string {\n" +
				"\tq := \"" + rangeSeed + "\"\n\tfor _, q = range arr {\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			"a parameter's array length is in the signature"},
		{"named-result",
			"package db\n\nfunc f() (arr [2]string, q string) {\n" +
				"\tq = \"" + rangeSeed + "\"\n\tfor _, q = range arr {\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn arr, q\n}\n",
			"a named result is declared in the signature too"},
		{"captured-from-an-enclosing-function",
			"package db\n\nfunc f() func() string {\n\tvar arr [2]string\n" +
				"\treturn func() string {\n\t\tq := \"" + rangeSeed + "\"\n" +
				"\t\tarr = [2]string{}\n\t\tfor _, q = range arr {\n\t\t}\n" +
				"\t\tq += \" FOR UPDATE\"\n\t\treturn q\n\t}\n}\n",
			"the closure is its own fold scope, so the assignment is the only " +
				"evidence of the array it captured"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if worlds := foldOnly(foldTexts(t, c.src), rangeSeed); len(worlds) != 0 {
				t.Fatalf("the loop certainly overwrites q (%s), so this world is a "+
					"query no path produces; got %q", c.why, worlds)
			}
		})
	}
}

// THE NARROWING, and the half that stops this fix from paying #72's price. Every
// source here MAY iterate zero times, so the world built from the appends
// outside the loop is a path the program really has and the finding on it is a
// true positive. Deleting these was the measured cost that killed the first
// design; a fix that cannot tell them apart is worse than the defect.
func TestRangeOverAPossiblyEmptySourceStillFolds(t *testing.T) {
	cases := []struct{ name, body, why string }{
		{"map-variable", "\tfor q = range m {\n\t}\n",
			"an empty map iterates zero times, so q survives the loop"},
		{"slice-variable", "\tvar xs []string\n\tfor _, q = range xs {\n\t}\n",
			"a nil or empty slice iterates zero times"},
		{"channel", "\tvar ch chan string\n\tfor q = range ch {\n\t}\n",
			"a closed empty channel iterates zero times"},
		{"iterator-function", "\tvar seq func(func(string) bool)\n\tfor q = range seq {\n\t}\n",
			"an iterator function may yield nothing"},
		{"zero-length-array", "\tvar arr [0]string\n\tfor _, q = range arr {\n\t}\n",
			"[0]string iterates zero times — length is what decides, not the word array"},
		{"empty-composite-literal", "\tfor _, q = range []string{} {\n\t}\n",
			"an empty slice literal iterates zero times"},
		{"define-rebinds", "\tfor _, q := range m {\n\t\t_ = q\n\t}\n",
			"`:=` binds a new q inside the loop; the outer one is never written"},
		{"define-over-a-non-empty-source", "\tfor _, q := range []string{\"a\"} {\n\t\t_ = q\n\t}\n",
			"`:=` still binds a new q when the loop certainly runs — a source that " +
				"cannot be empty says nothing about which q is written"},
		{"name-bound-to-a-slice-literal", "\txs := []string{\"a\"}\n" +
			"\tfor _, q = range xs {\n\t}\n",
			"a SLICE variable seeded non-empty can be reassigned empty before the " +
				"loop, and this pass reads no statement order — only an array's " +
				"length is fixed by its type"},
		{"array-declared-inside-a-closure", "\tvar xs []string\n" +
			"\tinner := func() {\n\t\tvar xs [2]string\n\t\t_ = xs\n\t}\n\t_ = inner\n" +
			"\tfor _, q = range xs {\n\t}\n",
			"the [2]string belongs to the closure's own scope; the xs this loop " +
				"ranges over is a slice and may be empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := foldTexts(t, rangeSrc(c.body))
			if !hasFoldText(got, rangeSeed+" FOR UPDATE") {
				t.Fatalf("this world is a real path (%s) and a finding on it is a "+
					"true positive; it must still be emitted, got %q",
					c.why, foldOnly(got, rangeSeed))
			}
		})
	}
}
