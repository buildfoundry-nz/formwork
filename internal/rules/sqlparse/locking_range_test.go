// locking_range_test.go — #314 at the gate.
//
// The fold's verdict on a `for … = range` clause is only interesting because
// sql/locking-select-order reads it. Both directions are here, because #314's
// fix has a cost in each: firing on a query no path produces asks a developer to
// justify a deadlock finding that is not real, and dropping one that a path DOES
// produce is silence on a deadlock hazard — the direction this whole rule exists
// to avoid.
//
// internal/sqlextract's own tests say which worlds the fold EMITS; these say
// which of them the gate FIRES on.
package sqlparse_test

import "testing"

// The issue's reproduction, at the layer it was reported from. `var arr
// [2]string` iterates twice, so q after the loop is "" and the function returns
// " FOR UPDATE" — no query at all, and certainly not an unordered locking
// SELECT. The gate reported one.
func TestLockingRangeOverAFixedArrayDoesNotFire(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc r() string {\n" +
		"\tvar arr [2]string\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tfor _, q = range arr {\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("r.go", src)); len(ms) != 0 {
		t.Fatalf("the loop overwrites q twice, so the query this fires on is one "+
			"no execution path produces: %+v", ms)
	}
}

// The other direction, and the one #72's third comment adjudicated. An empty map
// iterates zero times, so q survives the loop and the program really can return
// an unordered locking SELECT. A fix that untracked every range clause would
// take this finding away — that was measured at 8 true positives, and it is why
// the fix reads the range SOURCE rather than the statement.
func TestLockingRangeOverAMapStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc r(m map[string]string) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tfor q = range m {\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("r.go", src)); len(ms) != 1 {
		t.Fatalf("the zero-iteration path returns this exact unordered locking "+
			"SELECT; want 1 finding: %+v", ms)
	}
}
