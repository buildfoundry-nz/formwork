// fold_named_closure_test.go — #72, at its real shape.
//
// The issue reports that a closure's appends are dropped while the variable
// stays tracked, using an IIFE as its example. Probed at HEAD, the IIFE case is
// already handled by the inlining work; the live defect is a closure BOUND TO A
// NAME and then called:
//
//	f := func() { q += " ORDER BY id" }
//	f()
//
// untrackAssigned returns false on *ast.FuncLit, so f's append is neither
// folded nor untracked: q stays tracked, the write is dropped, and the fold
// emits a world assembled from only the appends outside f — one no execution
// path produces.
//
// Not every closure is this. One that is never called, or called
// conditionally, or created after the value is used, makes the outside-appends
// world a REAL path, and emitting it is correct. That is why the fix keys on
// the closure being invoked in the same scope rather than on its existence.
package sqlextract_test

import (
	"strings"
	"testing"
)

func TestNamedClosureCalledDoesNotFabricateAWorld(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tf := func() { q += \" ORDER BY id\" }\n" +
		"\tf()\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	seed := "SELECT id FROM t WHERE s = 'x'"
	for _, got := range foldOnly(foldTexts(t, src), seed) {
		if strings.Contains(got, "FOR UPDATE") && !strings.Contains(got, "ORDER BY") {
			t.Fatalf("f() runs, so this world exists on no path: %q", got)
		}
	}
}

// The other direction, and the dangerous one: the closure adds the LOCK.
func TestNamedClosureCalledDoesNotFabricateAnOrderedWorld(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tf := func() { q += \" FOR UPDATE\" }\n" +
		"\tf()\n" +
		"\tq += \" ORDER BY id\"\n" +
		"\treturn q\n}\n"
	seed := "SELECT id FROM t WHERE s = 'x'"
	for _, got := range foldOnly(foldTexts(t, src), seed) {
		if strings.Contains(got, "ORDER BY") && !strings.Contains(got, "FOR UPDATE") {
			t.Fatalf("emitted a world assembled from only the visible appends: %q", got)
		}
	}
}

// The narrowing, and it is the whole reason this keys on invocation. A closure
// that is NEVER called leaves the outside-appends world real, and untracking
// here would delete a true positive — a locking SELECT with no ORDER BY that
// the code genuinely produces.
func TestNamedClosureNeverCalledStillFolds(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\tf := func() { q += \" ORDER BY id\" }\n" +
		"\t_ = f\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if !hasFoldText(foldTexts(t, src), "SELECT id FROM t FOR UPDATE") {
		t.Fatalf("an uncalled closure leaves the outside world real; got %q",
			foldOnly(foldTexts(t, src), "SELECT id FROM t"))
	}
}
