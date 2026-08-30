// fold_unseen_write_test.go — #72 and #74.
//
// One root defect, two spellings: THE FOLD DROPS A WRITE WHILE KEEPING THE
// VARIABLE TRACKED. It then emits a value assembled from only the writes it
// happened to see — a query no execution path produces, which §4.1 promises
// never to emit.
//
// It breaks in both directions and the second is the dangerous one. With the
// unseen write adding the ORDER BY, the fold emits an UNORDERED locking SELECT
// that no path holds: a false positive. With the unseen write adding the lock,
// the fold emits an ordered query and a real deadlock hazard goes unreported.
//
// The fix is fail-closed at the seam that already exists: a write the fold
// cannot see UNTRACKS the variable, so nothing is emitted for it rather than
// something wrong. Untracking loses a candidate; fabricating loses the truth.
package sqlextract_test

import (
	"strings"
	"testing"
)

// #72 — untrackAssigned returned false on *ast.FuncLit, so a closure's append
// was neither folded nor untracked.
func TestClosureAppendDoesNotFabricateAWorld(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT * FROM t WHERE s='x'\"\n" +
		"\tq += \" AND y=1\"\n" +
		"\tfunc() { q += \" ORDER BY id\" }()\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	seed := "SELECT * FROM t WHERE s='x'"
	for _, got := range foldOnly(foldTexts(t, src), seed) {
		if strings.Contains(got, "FOR UPDATE") && !strings.Contains(got, "ORDER BY") {
			t.Fatalf("emitted an unordered locking SELECT the code never produces "+
				"(the closure's ORDER BY was dropped while q stayed tracked): %q", got)
		}
	}
}

// The other direction, and the one that matters most: the closure adds the
// LOCK. A fabricated ordered world here would hide a real hazard.
func TestClosureLockAppendDoesNotFabricateAnOrderedWorld(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT * FROM t WHERE s='x'\"\n" +
		"\tfunc() { q += \" FOR UPDATE\" }()\n" +
		"\tq += \" ORDER BY id\"\n" +
		"\treturn q\n}\n"
	seed := "SELECT * FROM t WHERE s='x'"
	for _, got := range foldOnly(foldTexts(t, src), seed) {
		if strings.Contains(got, "ORDER BY") && !strings.Contains(got, "FOR UPDATE") {
			t.Fatalf("emitted a world assembled from only the visible appends: %q", got)
		}
	}
}

// #74 — a write through a taken address is not an assignment to the identifier,
// so the fold never saw it.
func TestPointerWriteDoesNotFabricateAWorld(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tp := &q\n" +
		"\t*p += \" ORDER BY id\"\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	seed := "SELECT id FROM t WHERE s = 'x'"
	for _, got := range foldOnly(foldTexts(t, src), seed) {
		if strings.Contains(got, "FOR UPDATE") && !strings.Contains(got, "ORDER BY") {
			t.Fatalf("emitted an unordered locking SELECT that is ordered on every "+
				"real path (the *p write was dropped while q stayed tracked): %q", got)
		}
	}
}

// The narrowing that keeps this from deleting true positives. An ordinary
// composition with no unseen write must still fold — without this, "untrack
// whenever anything looks hard" would quietly stop the rule firing at all,
// which is the same silence in a different costume.
func TestOrdinaryCompositionStillFolds(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\tq += \" WHERE s = 'x'\"\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	if !hasFoldText(texts, "SELECT id FROM t WHERE s = 'x' FOR UPDATE") {
		t.Fatalf("an ordinary composition must still fold; got %q", texts)
	}
}

// Taking an address for READING is not a write, and must not untrack: `len(*p)`
// or passing &q to something that only reads it leaves every append visible.
// Untracking here would delete a true positive on a very common shape.
func TestAddressTakenButNotWrittenStillFolds(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\tp := &q\n" +
		"\t_ = len(*p)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	if !hasFoldText(texts, "SELECT id FROM t FOR UPDATE") {
		t.Fatalf("a read through a pointer must not untrack; got %q", texts)
	}
}
