package sqlparse_test

import "testing"

// Gate-level tests for queries the assignment-flow fold reassembles (#36, #42)
// — a locking SELECT composed across `q := <SELECT>` then `q += …`, with the
// lock in a later statement or inside an `if`, must be analyzed rather than
// silently passed. Modelled on a real allocation-commit shape in the validating target.
//
// Split out of locking_test.go under the 750-line vendor cap; that file keeps
// the tests that check the rule's own SQL analysis (unique-key exemption, join
// handling, ORDER BY resolution) against .sql input. The helpers (checker,
// matches, file) are shared across the package.
//
// These are the tests that answer "does the gate FIRE", as distinct from
// internal/sqlextract's "which worlds does the fold EMIT". Both layers matter:
// a world can be emitted and still not fire, and every capability here was at
// some point emitted-but-untested at the gate.

func TestLockingAssignmentComposedUnorderedFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(forUpdate bool) string {\n" +
		"\tq := `SELECT id FROM appdb.projects WHERE id = ANY($1)`\n" +
		"\tif forUpdate {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("commit.go", src)); len(ms) != 1 {
		t.Fatalf("assignment-composed unordered lock must fire: %+v", ms)
	}
}

func TestLockingAssignmentComposedOrderedPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(forUpdate bool) string {\n" +
		"\tq := `SELECT id FROM appdb.projects WHERE id = ANY($1)`\n" +
		"\tif forUpdate {\n\t\tq += \" ORDER BY id FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("commit.go", src)); len(ms) != 0 {
		t.Fatalf("assignment-composed ordered lock must pass: %+v", ms)
	}
}

func TestLockingOptionalOrderUnconditionalLockFires(t *testing.T) {
	// The lock is unconditional; the ORDER BY is optional. The path that skips
	// the order (base) is a real hazard and must fire — the case a naive
	// flatten-everything fold would miss.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(withOrder bool) string {\n" +
		"\tq := `SELECT id FROM t WHERE id = ANY($1)`\n" +
		"\tif withOrder {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("optional order + unconditional lock: the unordered path must fire: %+v", ms)
	}
}

func TestLockingDecorrelatedGuardsDoNotFire(t *testing.T) {
	// Two guards on the SAME condition split ORDER BY and FOR UPDATE. All-or-
	// nothing folding never emits the infeasible lock-without-order partial, so
	// this provably-safe query must not fire.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(forUpdate bool) string {\n" +
		"\tq := `SELECT id FROM t WHERE id = ANY($1)`\n" +
		"\tif forUpdate {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif forUpdate {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("decorrelated same-condition guards must not fire: %+v", ms)
	}
}

func TestLockingIfElseBothAppendUntracked(t *testing.T) {
	// Both branches append (a mandatory choice): the variable is untracked (a
	// sound, disclosed miss), so no folded candidate — and, crucially, no false
	// positive from a wrongly-skipped mandatory clause.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(fast bool) string {\n" +
		"\tq := `SELECT id FROM t WHERE id = ANY($1)`\n" +
		"\tif fast {\n\t\tq += \" ORDER BY id FOR UPDATE\"\n\t} else {\n\t\tq += \" ORDER BY id, ctid FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("if/else both-append is untracked and must not false-positive: %+v", ms)
	}
}

func TestLockingComposedDoesNotDoubleCount(t *testing.T) {
	// The seed already locks without order; a trailing `+= ";"` makes the fold
	// pass emit a superset candidate at the SAME line as the walk's seed
	// candidate. Both fire; without dedup they double-count. (#36)
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q() string {\n" +
		"\tsql := \"SELECT id FROM t WHERE id = ANY($1) FOR UPDATE\"\n" +
		"\tsql += \";\"\n" +
		"\treturn sql\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("seed+append duplicate at one line must count once: %+v", ms)
	}
}

func TestLockingSplitDefineComposedDoesNotDoubleCount(t *testing.T) {
	// A `:=` whose RHS literal is on the next line: the expression walk anchors
	// the seed candidate at the RHS line, so folding must anchor there too, or
	// the seed/fold duplicate lands on two different lines and escapes the
	// (Line,Message) dedup. (#36)
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q() string {\n\tsql :=\n\t\t\"SELECT id FROM t WHERE id = ANY($1) FOR UPDATE\"\n\tsql += \";\"\n\treturn sql\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("split := seed/fold duplicate must count once: %+v", ms)
	}
}

// #41: SKIP LOCKED never waits on a row another transaction holds — it skips
// it — so it can never be the waiting edge in a lock-wait cycle and cannot
// deadlock, whatever its ORDER BY. It must be exempt regardless of ordering.
func TestLockingSkipLockedExemptEvenUnordered(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x' FOR UPDATE SKIP LOCKED;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("FOR UPDATE SKIP LOCKED cannot deadlock; must not fire: %+v", ms)
	}
}

// The real queue-drainer shape from the pinned target:
// a non-unique ORDER BY under FOR UPDATE SKIP LOCKED. Exempt via SKIP LOCKED;
// the identical query WITHOUT it still fires, proving the exemption is load-bearing.
func TestLockingSkipLockedExemptWithNonUniqueOrder(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	skip := "SELECT id FROM projects WHERE sla_at_risk_escalated_at IS NULL ORDER BY sla_at_risk_at ASC LIMIT $1 FOR UPDATE SKIP LOCKED;\n"
	if ms := matches(t, c, file("q.sql", skip)); len(ms) != 0 {
		t.Fatalf("SKIP LOCKED queue-drainer must not fire: %+v", ms)
	}
	block := "SELECT id FROM projects WHERE sla_at_risk_escalated_at IS NULL ORDER BY sla_at_risk_at ASC LIMIT $1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", block)); len(ms) != 1 {
		t.Fatalf("the same non-unique ORDER BY without SKIP LOCKED must still fire: %+v", ms)
	}
}

// NOWAIT (LockWaitError) is deliberately NOT exempted here (#41 scope): it
// errors instead of waiting, so it also cannot deadlock, but the reasoning
// differs and is decided separately. Pin that it still fires so the SKIP LOCKED
// exemption is not widened to every non-blocking wait policy by accident.
func TestLockingNowaitStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x' FOR UPDATE NOWAIT;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("FOR UPDATE NOWAIT is out of the SKIP LOCKED exemption; must still fire: %+v", ms)
	}
}

// #42 REMAINS OPEN, DELIBERATELY. Every real path here is ordered, so the only
// firing candidate is the fold's `base` — the "neither complementary branch
// ran" world, which the pair proves unreachable. Suppressing it needs proof
// that the two reads see one value; four review rounds established that a
// parse-only pass cannot carry that proof, and that a wrongly proven pair
// deletes a reachable world and passes a real deadlock hazard. Between a
// visible false positive a `formwork:allow` marker clears and a silent false
// negative, the gate takes the false positive.
func TestLockingComplementaryGuardFoldBaseStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the disclosed #42 false positive: want the 1 base finding: %+v", ms)
	}
}

// THE CAPABILITY THIS BRANCH ADDS, ASSERTED AT THE GATE. A lock and an order
// split across ONE flag's opposite polarities: with x=true the executed query is
// `SELECT … FOR UPDATE` with no ORDER BY — a real deadlock hazard — and with
// x=false it is ordered and unlocked. BOTH all-or-nothing extremes are safe
// (`full` takes both appends and is ordered; `base` takes neither and does not
// lock), so before the branch worlds were emitted this fired NOTHING. That is
// spec §9's miss 1.
//
// This is the only test in the suite that fails if branch-world emission is
// removed from foldWorlds — the neighbouring complementary-guard tests all carry
// an UNCONDITIONAL `q += " FOR UPDATE"`, so their findings come from `base` and
// they pass on main too. Verified by deleting the emission and re-running: this
// test goes red, they stay green.
func TestLockingSplitLockAndOrderAcrossOneFlagFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(x bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif !x {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif x {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the x=true world is a locking SELECT with no ORDER BY; want 1 finding: %+v", ms)
	}
}

// Two complementary pairs on one variable. Neither all-or-nothing extreme is a
// hazard — `full` orders, `base` does not lock — so while the enumeration
// stopped at one pair this reported nothing, even though every b=false path is
// an unordered lock.
func TestLockingSecondGuardPairStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc build(a, b bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s='x'\"\n" +
		"\tif a {\n\t\tq += \" AND p = 1\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" AND r = 2\"\n\t}\n" +
		"\tif b {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !b {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) == 0 {
		t.Fatalf("every b=false path is an unordered lock; the rule must fire: %+v", ms)
	}
}

// The same hazard as the test above, wrapped in a transaction guard — and until
// the pair rule keyed on (prefix, last guard) this reported NOTHING, while the
// unwrapped version reported it. Wrapping a builder in `if useTx { … }` must not
// turn a caught hazard into a silent pass.
func TestLockingSplitLockAndOrderUnderSharedGuardFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc build(a, useTx bool) string {\n" +
		"\tq := \"SELECT * FROM t WHERE s='x'\"\n" +
		"\tif useTx {\n" +
		"\t\tif a {\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n" +
		"\t\tif !a {\n\t\t\tq += \" FOR UPDATE\"\n\t\t}\n" +
		"\t}\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the useTx&&!a world is an unordered lock; want 1 finding: %+v", ms)
	}
}

// The mirror of the test above: complementary branches that append DIFFERENT
// clauses. The `!a` path really executes `… LIMIT 10 FOR UPDATE` — a locking
// SELECT over sibling rows with no deterministic ORDER BY, the exact deadlock
// hazard this rule exists to catch. Suppressing the whole no-optional world and
// keeping only the both-branches text would report nothing here.
//
// Note this one ALSO fires from `base` (the unconditional lock below), so it
// does not on its own prove branch-world emission works —
// TestLockingSplitLockAndOrderAcrossOneFlagFires is what pins that.
func TestLockingComplementaryGuardOneBranchUnorderedFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" LIMIT 10\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the unordered complementary branch is a real violation, want 1 finding: %+v", ms)
	}
}

// A complementary pair must not silence an INDEPENDENT optional ORDER BY. With
// wantOrder=false the query locks without ordering on a genuinely reachable
// path, so the rule must still fire.
func TestLockingComplementaryPairWithIndependentOrderFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a, wantOrder bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" AND p = 1\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" AND r = 2\"\n\t}\n" +
		"\tif wantOrder {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) == 0 {
		t.Fatalf("the wantOrder=false path is an unordered lock; the rule must fire: %+v", ms)
	}
}

func TestLockingTwoUnorderedStatementsOneLineBothFire(t *testing.T) {
	// Two DISTINCT unordered locking SELECTs on one physical .sql line are two
	// real violations. The walk/fold dedup (#36) is .go-only, so it must not
	// collapse them here.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE s='x' FOR UPDATE; SELECT * FROM u WHERE s='y' FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 2 {
		t.Fatalf("two distinct one-line locking statements must both fire: %+v", ms)
	}
}

// The same disclosed limitation for the commonest guard shape in a query
// builder: an options STRUCT FIELD read twice. A field is even less provable
// than a bare flag — anything holding the struct can write it — so this is the
// shape the suppression attempts kept getting wrong in the silent direction.
func TestLockingSelectorGuardFoldBaseStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\ntype opt struct{ Ordered bool }\n\nfunc q(o opt) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the disclosed #42 false positive on a field guard: want the 1 base finding: %+v", ms)
	}
}

// THE PRICE OF ENUMERATING EVERY PAIR, disclosed and pinned. `b := a` makes the
// two flags one value, so no real path here is an unordered lock: a=b=true
// orders and locks, a=b=false does neither.
//
// Fixing b=true still emits a MINIMAL world in which the `a` pair's appends are
// off — `… FOR UPDATE` with no ORDER BY — and that world is unreachable, because
// b=true forces a=true forces the ORDER BY. So this fires, and it is a false
// positive.
//
// It is #42's class exactly, not a new one: the fold cannot prove a relationship
// between two flags, so it keeps the world rather than deleting it, and a
// deleted world is the failure that goes unseen. Note what it does NOT do —
// pairs are enumerated separately, never multiplied, so the mixed-flag world
// `… LIMIT 1 FOR UPDATE` is still never invented
// (TestFromGoReassembledSeveralPairsInventNoMixedWorld). A `formwork:allow`
// marker clears this one, and formwork lint keeps the marker in view.
func TestLockingCorrelatedGuardPairsFireAsDisclosed(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tb := a\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" LIMIT 1\"\n\t}\n" +
		"\tif b {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\tif !b {\n\t\tq += \" OFFSET 2\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the disclosed correlated-flag false positive: want the 1 finding: %+v", ms)
	}
}

// Losing the complementarity proof must not lose the hazard. A benign helper
// call handed the options struct makes the pair unprovable, and the
// o.Ordered=true path is then a locking SELECT with no ORDER BY: the gate must
// still report it, not fall back to a world set that happens to be clean.
func TestLockingUnprovablePairStillFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\ntype opt struct{ Ordered bool }\n\nfunc col(o *opt) string { return \"id\" }\n\n" +
		"func q(o *opt) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tif !o.Ordered {\n\t\tq += \" ORDER BY \" + col(o)\n\t}\n" +
		"\tif o.Ordered {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the o.Ordered=true world is an unordered lock, want 1 finding: %+v", ms)
	}
}

// A call handed the guard's struct can mutate it, so "neither branch ran" is
// reachable and that path locks without ordering. Requiring a literal `&` to see
// the write hides the commonest options-struct shape there is.
func TestLockingGuardBaseHandedToCallFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\ntype opt struct{ Ordered bool }\n\nfunc normalize(o *opt) {}\n\n" +
		"func q(o *opt) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tif o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tnormalize(o)\n" +
		"\tif !o.Ordered {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the call may flip the guard; the no-optional world is an unordered lock: %+v", ms)
	}
}

// The false-negative mirror: a guard reassigned by a range clause between its
// branches leaves "neither branch ran" reachable, and that path is an unordered
// locking SELECT — a real deadlock hazard the gate must report.
func TestLockingGuardReassignedByRangeFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool, xs []bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tfor _, a = range xs {\n\t}\n" +
		"\tif !a {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("the reachable no-optional world is an unordered lock, want 1 finding: %+v", ms)
	}
}

func TestLockingIIFEOrderedDoesNotFire(t *testing.T) {
	// #72's noisy direction at the gate. The ORDER BY lives in an
	// immediately-invoked closure, so the query is ordered on every real path.
	// Before the inline fold, the gate reported an unordered locking SELECT on
	// it and asked a developer to justify a finding that was not real.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load() string {\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tq += \" AND y = 1\"\n" +
		"\tfunc() { q += \" ORDER BY id\" }()\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("a query ordered inside an IIFE must not fire: %+v", ms)
	}
}

func TestLockingIIFELockFires(t *testing.T) {
	// #72's SILENT direction at the gate, and the whole point of the change.
	// The FOR UPDATE lives in an immediately-invoked closure, so the real value
	// is an unordered locking SELECT — a genuine deadlock hazard that reported
	// NOTHING before, because the fold dropped the closure's append while
	// keeping the variable tracked and then passed the value it invented.
	//
	// This asserts 1, not 0. An earlier design (spec §10, rejected) fixed the
	// fabrication by refusing the variable, which left this at 0 and called it a
	// disclosed miss. Refusing can only ever subtract findings — the gate fires
	// per emitted candidate — so it could never have caught this.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load() string {\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tq += \" AND y = 1\"\n" +
		"\tfunc() { q += \" FOR UPDATE\" }()\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an unordered lock added inside an IIFE must fire: %+v", ms)
	}
}

func TestLockingIIFEMandatoryIfElseStillFires(t *testing.T) {
	// Found on re-review, round 2 — and the reason the defect got through:
	// nothing at the gate exercised an if/else (a mandatory, both-branches-
	// append choice) inside an IIFE. iifeModellable was accepting the shape
	// by recursing into both branches, but foldStmts does not fold it that
	// way — a mandatory if/else is untracked WHOLESALE (spec §4.2) — and
	// applied inside an inlined body that untrack is no longer sealed at the
	// closure boundary: it deleted the outer q outright, silencing even the
	// post-closure FOR UPDATE finding base correctly reported.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(b bool) string {\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tif b {\n\t\t\tq += \" ORDER BY id\"\n\t\t} else {\n\t\t\tq += \" AND z = 1\"\n\t\t}\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an if/else inside an IIFE must not silence the outer, post-closure lock: %+v", ms)
	}
}

func TestLockingIIFEIfWithInitStillFires(t *testing.T) {
	// Found on re-review, round 3 — the same root cause as the if/else
	// finding, one level deeper. An if WITHOUT an else but WITH an Init looks
	// just as foldable as an ordinary if-without-else, but foldStmts
	// untracks Init unconditionally: a `:=` there is a CLOSURE-LOCAL shadow
	// (the if's Init clause opens its own scope), but untrackAssigned
	// deletes by bare name, so it deleted the outer q and silenced the
	// post-closure lock finding base correctly reported.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load() string {\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() {\n\t\tif q := \"x\"; q != \"\" {\n\t\t\t_ = q\n\t\t}\n\t}()\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an if with an Init inside an IIFE must not silence the outer, post-closure lock: %+v", ms)
	}
}

// iifeGateSrc wraps one IIFE body between the seed and the post-closure lock, so
// the gate tests below differ only in the closure body under test.
func iifeGateSrc(body string) string {
	return "package db\n\nfunc load(b bool) string {\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" + body +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
}

func TestLockingIIFEClosureLocalWriteStillFires(t *testing.T) {
	// Found on re-review, round 3, ruled on in round 4 — and the reason it got
	// through three rounds: nothing at the gate exercised a write inside an
	// inlined body at all. Every shape here really produces
	// `SELECT id FROM t WHERE s = 'x' FOR UPDATE` — an unordered locking SELECT,
	// the hazard this rule exists for. Base reports it; rounds 0-3 reported
	// NOTHING, because foldAssign untracked the outer q on a statement that
	// provably never wrote it (a `:=` binds in the closure's own block) or might
	// have (`=`, which now disqualifies the body instead of deleting the
	// variable). Each subtest measured base 1 → head 0 before this round.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	for _, tc := range []struct{ name, body string }{
		{"define", "\tfunc() { q := \"SELECT 2\"; _ = q }()\n"},
		{"multi-name define", "\tfunc() { a, q := \"1\", \"2\"; _, _ = a, q }()\n"},
		{"define inside an if", "\tfunc() {\n\t\tif b {\n\t\t\tq := \"z\"\n\t\t\t_ = q\n\t\t}\n\t}()\n"},
		{"assign", "\tfunc() { q = \"SELECT 2\" }()\n"},
		{"assign inside an if", "\tfunc() {\n\t\tif b {\n\t\t\tq = \"z\"\n\t\t}\n\t}()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ms := matches(t, c, file("q.go", iifeGateSrc(tc.body))); len(ms) != 1 {
				t.Fatalf("a write inside an IIFE must not silence the outer, post-closure lock: %+v", ms)
			}
		})
	}
}

func TestLockingIIFEShadowedAppendStillFires(t *testing.T) {
	// The hazard skipping a `:=` opens. The closure builds its OWN q and orders
	// it; the outer q is never touched, so the real outer value is an unordered
	// locking SELECT and base reports it. Folding the closure-local append into
	// the outer q instead would make the outer value read as ordered and silence
	// this — the same 1 → 0 the three rounds before it produced, reached through
	// a fabrication rather than an untrack.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := iifeGateSrc("\tfunc() {\n\t\tq := `SELECT other FROM u`\n" +
		"\t\tq += \" ORDER BY id\"\n\t\t_ = q\n\t}()\n")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("a closure-local query's ORDER BY must not silence the outer lock: %+v", ms)
	}
}

func TestLockingIIFEMalformedAppendStillFires(t *testing.T) {
	// `+=` is the one token the inlined-body skip guard passes through to the
	// fold, and go/parser accepts a multi-target `+=` — a type error, not a parse
	// error — which reaches foldAssign's multi-LHS untrack and deletes the outer
	// q. This rule reads files it never type-checks, so a .go file that does not
	// build must still not silence the lock the file that does build would report.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := iifeGateSrc("\tfunc() { a, q += \"1\", \"2\"; _, _ = a, q }()\n")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an append this pass cannot fold must not silence the outer, post-closure lock: %+v", ms)
	}
}

func TestLockingIIFEBareNonLiteralAppendStillFires(t *testing.T) {
	// Found by the base-equality differential, round 5. An append carrying no
	// literal text renders as the bare "fw_expr" placeholder, glued straight
	// onto the accumulated text: `…WHERE s = 'x'` + `fw_expr` parses as nothing,
	// so the only candidate for q is dropped and the post-closure FOR UPDATE
	// base reported goes unreported. Silence on a deadlock hazard, arrived at by
	// folding MORE honestly than base — the one direction this task must never
	// take.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := iifeGateSrc("\tbuildClause := func() string { return \"\" }\n" +
		"\tfunc() { q += buildClause() }()\n")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an append with no literal text must not silence the outer, post-closure lock: %+v", ms)
	}
}

func TestLockingIIFEMixedAppendLockStillFires(t *testing.T) {
	// GUARD for the narrowing above, and gate-observable in the direction that
	// matters: there is no post-closure append here, so base — with the literal
	// opaque — records nothing for q at all and reports NOTHING. The finding
	// exists only because the mixed append is folded inline. Disqualifying a
	// mixed append along with a bare one would take this straight back to
	// silence on a lock, which is why the narrowing is keyed on "no literal text
	// anywhere" and not on "any placeholder".
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc load(b bool) string {\n" +
		"\ttbl := \"t\"\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() { q += \" FOR UPDATE OF \" + tbl }()\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("a mixed literal/non-literal append must still be folded, so the lock it adds fires: %+v", ms)
	}
}

func TestLockingIIFEPlaceholderOnlyAppendStillFires(t *testing.T) {
	// Found by the base-equality differential, round 6. An empty literal and a
	// bare `%s` format each carry a literal TOKEN, which is all the previous
	// check asked for, and each renders to nothing but the `fw_expr`
	// placeholder — byte for byte what `q += col` renders to, and the same
	// silence: glued onto the accumulated value it parses as nothing, so the
	// post-closure FOR UPDATE base reported goes unreported.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	for _, tc := range []struct{ name, expr string }{
		{"empty literal", "\"\" + col"},
		{"bare format verb", "fmt.Sprintf(\"%s\", col)"},
		{"leading space", "\" \" + col"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package db\n\nimport \"fmt\"\n\nfunc load(b bool) string {\n" +
				"\tcol := \"id\"\n\t_ = fmt.Sprint\n" +
				"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
				"\tfunc() { q += " + tc.expr + " }()\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
			if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
				t.Fatalf("an append rendering to nothing but a placeholder must not silence the outer lock: %+v", ms)
			}
		})
	}
}

func TestLockingIIFERenderedContentAppendLockStillFires(t *testing.T) {
	// GUARD for the narrowing above, gate-observable in the direction that
	// matters: with no post-closure append, base leaves the literal opaque,
	// records nothing for q and reports NOTHING. The finding exists only
	// because an append whose rendering carries real content — ` FOR UPDATE OF `
	// survives removing the placeholder — is folded inline. Narrowing from
	// "renders to content" to "renders to no placeholder at all" would take this
	// back to silence on a lock.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nimport \"fmt\"\n\nfunc load(b bool) string {\n" +
		"\ttbl := \"t\"\n\t_ = fmt.Sprint\n" +
		"\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
		"\tfunc() { q += fmt.Sprintf(\" FOR UPDATE OF %s\", tbl) }()\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("an append with real rendered content must still be folded, so the lock it adds fires: %+v", ms)
	}
}

func TestLockingIIFELeadingPlaceholderAppendStillFires(t *testing.T) {
	// The last two shapes the base-equality differential lost, at the gate. Each
	// renders to text STARTING with the placeholder, which the fold glues onto
	// the accumulated value's last token — `…s = 'x'` + `fw_expr` — so the only
	// candidate for q parses as nothing and the post-closure FOR UPDATE base
	// reported goes unreported. Content after the placeholder cannot rescue a
	// parse that has already failed to its left.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	for _, tc := range []struct{ name, call string }{
		{"concatenation", "\tfunc() { q += clause + \" ASC\" }()\n"},
		{"concatenation, nested", "\tfunc() { func() { q += clause + \" ASC\" }() }()\n"},
		{"leading format verb", "\tfunc() { q += fmt.Sprintf(\"%s ORDER BY id\", clause) }()\n"},
		{"leading format verb, nested", "\tfunc() { func() { q += fmt.Sprintf(\"%s ORDER BY id\", clause) }() }()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package db\n\nimport \"fmt\"\n\nfunc load(b bool) string {\n" +
				"\tclause := \"id\"\n\t_ = fmt.Sprint\n" +
				"\tq := `SELECT id FROM t WHERE s = 'x'`\n" + tc.call +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
			if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
				t.Fatalf("an append whose rendering starts with a placeholder must not silence the outer lock: %+v", ms)
			}
		})
	}
}

func TestLockingIIFEContentBeforePlaceholderLockStillFires(t *testing.T) {
	// GUARD for the position rule, gate-observable in the direction that
	// matters: with no post-closure append, base leaves the literal opaque,
	// records nothing for q and reports NOTHING. The finding exists only because
	// an append whose literal text comes BEFORE its placeholder is folded
	// inline. Tightening the rule from "content before the first placeholder" to
	// "no placeholder at all" would take both of these back to silence on a lock.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	for _, tc := range []struct{ name, call string }{
		{"concatenation", "\tfunc() { q += \" FOR UPDATE OF \" + tbl }()\n"},
		{"format string, nested", "\tfunc() { func() { q += fmt.Sprintf(\" FOR UPDATE OF %s\", tbl) }() }()\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package db\n\nimport \"fmt\"\n\nfunc load(b bool) string {\n" +
				"\ttbl := \"t\"\n\t_ = fmt.Sprint\n" +
				"\tq := `SELECT id FROM t WHERE s = 'x'`\n" + tc.call +
				"\treturn q\n}\n"
			if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
				t.Fatalf("an append with content before its placeholder must still be folded, so the lock it adds fires: %+v", ms)
			}
		})
	}
}
