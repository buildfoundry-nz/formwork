// locking_closure_call_test.go — #337 at the GATE.
//
// #72's fix had gate coverage of exactly one spelling of "this closure ran".
// Write the same unconditional call as a bare block, an alias, an
// immediately-invoked literal or an assignment value and the fold emitted an
// unordered locking SELECT the code never produces, so this rule reported a
// deadlock hazard on provably safe code — the FP #337 was filed for, end to end.
//
// WHAT THIS LAYER CAN SEE. The fix turns a WRONG EMIT into a NON-EMIT, and only
// one of the two directions moves a match count: the closure adding the ORDER BY
// goes 1 -> 0 here, while the closure adding the LOCK is 0 -> 0 (a missing
// candidate and a candidate that passes both read as zero findings). The second
// direction is pinned where it is visible, at the fold
// (internal/sqlextract/fold_closure_call_test.go).
//
// The controls and the narrowings are the other half of the file and they pass
// before the fix as well as after. They are what fails if the invocation walk
// untracks where the call is NOT proven to run, which is the design spec §10
// records as rejected for deleting eight true positives.
package sqlparse_test

import (
	"strings"
	"testing"
)

const closureSeed = "\tq := `SELECT id FROM t WHERE id = ANY($1)`\n"
const closureAdd = "\tadd := func() { q += \" ORDER BY id\" }\n"
const closureLock = "\tq += \" FOR UPDATE\"\n"

// closureSrc wraps a block in a file that declares both a helper that calls its
// argument and one that does not — see TestLockingClosureNameHandedToAHelperFires.
func closureSrc(body string) string {
	return "package db\n\nfunc exec(s string) {}\n" +
		"func run(f func()) { f() }\nfunc register(f func()) {}\n\n" +
		"type hooks struct{ order func() }\n\n" +
		"func load(b bool, xs []int, ch chan int) string {\n" + body + "\treturn q\n}\n"
}

// The false positive, at every spelling of the call the pass can see. The
// closure orders the query on every path, so the real value is a safe ordered
// locking SELECT and this rule must be silent.
func TestLockingProvablyCalledClosureDoesNotFire(t *testing.T) {
	for _, tc := range []struct{ name, call string }{
		{"a bare block", "\t{\n\t\tadd()\n\t}\n"},
		{"an alias", "\tg := add\n\tg()\n"},
		{"an alias chain", "\tg := add\n\th := g\n\th()\n"},
		{"an immediately-invoked literal", "\tfunc() { add() }()\n"},
		{"a labelled call", "\tif b {\n\t\tgoto call\n\t}\ncall:\n\tadd()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
			src := closureSrc(closureSeed + closureAdd + tc.call + closureLock)
			if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
				t.Fatalf("the closure orders the query on every path; firing here reports "+
					"a hazard the code does not have: %+v", ms)
			}
		})
	}
}

// The control, and it is the same source with the call spelled as a non-call.
// Without it every assertion above is satisfied by a fix that untracks the query
// outright and silences the rule.
func TestLockingUncalledClosureInTheSameShapeStillFires(t *testing.T) {
	for _, tc := range []struct{ name, call string }{
		{"a bare block", "\t{\n\t\t_ = add\n\t}\n"},
		{"an alias", "\tg := add\n\t_ = g\n"},
		{"an alias chain", "\tg := add\n\th := g\n\t_ = h\n"},
		{"an immediately-invoked literal", "\tfunc() { _ = add }()\n"},
		{"a labelled call", "\tif b {\n\t\tgoto call\n\t}\ncall:\n\t_ = add\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
			src := closureSrc(closureSeed + closureAdd + tc.call + closureLock)
			if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
				t.Fatalf("the closure never runs, so this really is an unordered locking "+
					"SELECT and must fire: %+v", ms)
			}
		})
	}
}

// The bindings, separately: a `var` binding is a closure too, and before #337
// only an assignment was read as one.
func TestLockingVarBoundClosureCalledDoesNotFire(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := closureSrc(closureSeed + "\tvar add = func() { q += \" ORDER BY id\" }\n\tadd()\n" + closureLock)
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("a var-bound closure is a closure; firing here reports a hazard the "+
			"code does not have: %+v", ms)
	}
}

func TestLockingVarBoundClosureNeverCalledStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := closureSrc(closureSeed + "\tvar add = func() { q += \" ORDER BY id\" }\n\t_ = add\n" + closureLock)
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an uncalled var-bound closure leaves a real unordered locking "+
			"SELECT that must fire: %+v", ms)
	}
}

// THE DISCLOSED FALSE POSITIVE, pinned rather than left to be rediscovered.
// #337's headline is `run(add)`, and it still fires. The closure's name escapes
// into a call, and whether the callee invokes f or stores it is cross-function
// flow spec §2 declines. `register` here never calls f, so for IT the unordered
// locking SELECT is the real value and this finding is a TRUE positive; the two
// call sites are one program to a parse-only pass, so silencing one silences
// both. The anonymous spelling of the same program is pinned green by
// internal/sqlextract/fold_iife_test.go's load-bearing guard, and splitting the
// two is the design spec §10 records as rejected.
func TestLockingClosureNameHandedToAHelperFires(t *testing.T) {
	for _, tc := range []struct{ name, call string }{
		{"a helper that calls it", "\trun(add)\n"},
		{"a helper that does not", "\tregister(add)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
			src := closureSrc(closureSeed + closureAdd + tc.call + closureLock)
			ms := matches(t, c, file("q.go", src))
			if len(ms) != 1 {
				t.Fatalf("both call sites are one program to a parse-only pass, and for "+
					"register() the hazard is real: %+v", ms)
			}
			if !strings.Contains(ms[0].Message, "ORDER BY") {
				t.Fatalf("unexpected finding: %+v", ms)
			}
		})
	}
}

// The narrowing at the gate. In each of these the closure is not proven to run
// before the value is used, so the unordered locking SELECT is a real path and
// the finding is a true positive this fix must not delete.
func TestLockingClosureNotProvablyCalledStillFires(t *testing.T) {
	for _, tc := range []struct{ name, call string }{
		{"deferred", "\tdefer add()\n"},
		{"started in a goroutine", "\tgo add()\n"},
		{"in a range body", "\tfor range xs {\n\t\tadd()\n\t}\n"},
		{"in a switch case", "\tswitch {\n\tcase b:\n\t\tadd()\n\t}\n"},
		{"in a select clause", "\tselect {\n\tcase <-ch:\n\t\tadd()\n\t}\n"},
		{"conditionally", "\tif b {\n\t\tadd()\n\t}\n"},
		{"through a struct field", "\th := hooks{order: add}\n\th.order()\n"},
		{"through a slice element", "\tfs := []func(){add}\n\tfs[0]()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
			src := closureSrc(closureSeed + closureAdd + tc.call + closureLock)
			if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
				t.Fatalf("the closure is not proven to have run, so the unordered locking "+
					"SELECT is a real path and must fire: %+v", ms)
			}
		})
	}
}
