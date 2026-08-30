// locking_escape_test.go — #310 at the GATE, which #74 never reached.
//
// #74's own Reproduce/verify asked for "a sql/locking-select-order case", and
// reverting #74 entirely at HEAD reddened exactly one test — a fold-level one
// in internal/sqlextract — while this whole package stayed `ok`. So the rule
// that the taken-address work exists to protect had no coverage of it at all.
//
// WHAT THIS LAYER CAN AND CANNOT SEE, stated because a test that cannot fail is
// the defect this file is answering. The fix turns a WRONG EMIT into a
// NON-EMIT, and only one of those two directions is visible in a match count:
//
//   - helper adds the ORDER BY, caller adds the lock: the fold emits an
//     unordered locking SELECT the code never produces and the rule FIRES on
//     provably safe code. 1 -> 0 here. That is TestLockingHelperGivenAddressDoesNotFire.
//   - helper adds the LOCK: the fold emits a non-locking world, so the rule is
//     silent — and after the fix the variable is untracked, so it is silent
//     again. 0 -> 0. No assertion on this package's output can tell those apart;
//     the difference is WHICH candidate texts were emitted, which is the fold
//     layer's question and is pinned there
//     (internal/sqlextract/fold_escape_test.go).
//
// The other three tests are the narrowing. They pass before the fix and must
// keep passing after it: they are what fails if the escape classifier untracks
// where the escape is NOT proven to have run, which is the design spec §10
// records as rejected for deleting eight true positives.
package sqlparse_test

import "testing"

const escapeSeed = "\tq := `SELECT id FROM t WHERE id = ANY($1)`\n"

// The false positive. The helper orders the query on every path, so the real
// value is a safe ordered locking SELECT; the fold's world omits the ORDER BY
// because the write goes through a pointer this pass cannot follow.
func TestLockingHelperGivenAddressDoesNotFire(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc orderIt(p *string) { *p += \" ORDER BY id\" }\n\n" +
		"func load() string {\n" + escapeSeed +
		"\torderIt(&q)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("the ORDER BY is applied on every path through a pointer this "+
			"pass cannot follow; firing here reports a hazard the code does not "+
			"have: %+v", ms)
	}
}

// The control, and it is the same source with ONE line removed. Without it the
// test above passes for the wrong reason — a fix that untracked every query
// would satisfy it and silence the rule.
func TestLockingCompositionWithoutTheEscapeStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc orderIt(p *string) { *p += \" ORDER BY id\" }\n\n" +
		"func load() string {\n" + escapeSeed +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("drop the escape and the identical composition is an unordered "+
			"locking SELECT that must still fire: %+v", ms)
	}
}

// A loop body runs zero times on an empty slice, so the un-decorated value is a
// real path and the hazard on it is a true positive.
func TestLockingEscapeInALoopBodyStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(xs []int) string {\n" + escapeSeed +
		"\tfor range xs {\n\t\torderIt(&q)\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an empty slice runs the body zero times, so the unordered "+
			"locking SELECT is a real path and must fire: %+v", ms)
	}
}

// Taking an address only to READ through it is not a write, and this block's
// own text proves p names q — so #74's deref-write analysis governs it and the
// query stays analysed.
func TestLockingResolvableAliasReadStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load() string {\n" + escapeSeed +
		"\tp := &q\n" +
		"\t_ = len(*p)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("a read through a pointer leaves every append visible; the "+
			"unordered locking SELECT must still fire: %+v", ms)
	}
}
