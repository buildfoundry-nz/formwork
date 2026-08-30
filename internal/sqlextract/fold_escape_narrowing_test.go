// fold_escape_narrowing_test.go — #310's other half, and the half that decides
// whether the fix is worth having.
//
// "Untrack whenever an address is taken" is the design spec §10 records as
// REJECTED: it removed ten findings, eight of them true positives, because
// under a branch the not-taken path is real — the world assembled WITHOUT the
// callee's writes is a genuine execution path, and a finding on it is a true
// positive. Deleting those trades a fabrication for a silence, which in this
// rule is a silenced deadlock.
//
// So an escape untracks only where it PROVABLY RUNS, and §4.1 states that as
// two independent conditions: the enclosing branch context, and the position
// inside the statement. The second is not implied by the first — a loop body
// runs zero times on an empty slice, a `switch` case may match nothing, a
// `select` clause is one of several, a `defer`/`go` call has not run when the
// value reaches the driver, and a `return` expression evaluates after every
// append, so the value being returned is exactly the one the fold assembled.
//
// Every test here is a world that MUST still be emitted. Each is also the only
// thing standing between this fix and §10's measured mistake, so each names the
// position it pins rather than sharing one table.
package sqlextract_test

import "testing"

// stillFolds asserts the fold still emits seed+tail — the escape sits where it
// is not proven to have run before the value is used, so the visible-appends
// world is real and a finding on it is a true positive.
func stillFolds(t *testing.T, src, seed, tail string) {
	t.Helper()
	texts := foldTexts(t, src)
	if !hasFoldText(texts, seed+tail) {
		t.Fatalf("this world is real and must still be emitted: %q not in %q", seed+tail, texts)
	}
}

const narrowSeed = "SELECT id FROM t WHERE s = 'x'"

func wrap(body string) string {
	return "package db\n\nfunc f(c bool, xs []int, n int, ch chan int, cfg *conf) string {\n" +
		"\tq := \"" + narrowSeed + "\"\n" + body + "\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
}

// A loop body runs zero times on an empty slice, so the un-decorated value is a
// real path.
func TestEscapeInALoopBodyStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tfor range xs {\n\t\tlockIt(&q)\n\t}\n"), narrowSeed, " FOR UPDATE")
}

// A three-clause `for` is the same argument: the body may never execute.
func TestEscapeInAThreeClauseForBodyStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tfor i := 0; i < n; i++ {\n\t\tlockIt(&q)\n\t}\n"), narrowSeed, " FOR UPDATE")
}

// A `switch` case may match nothing.
func TestEscapeInASwitchCaseStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tswitch n {\n\tcase 1:\n\t\tlockIt(&q)\n\t}\n"), narrowSeed, " FOR UPDATE")
}

// A type switch is a switch.
func TestEscapeInATypeSwitchArmStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tswitch v := any(n).(type) {\n\tcase int:\n\t\t_ = v\n\t\tlockIt(&q)\n\t}\n"), narrowSeed, " FOR UPDATE")
}

// A `select` clause is one of several; the others are equally real.
func TestEscapeInASelectClauseStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tselect {\n\tcase <-ch:\n\t\tlockIt(&q)\n\tdefault:\n\t}\n"), narrowSeed, " FOR UPDATE")
}

// A deferred call has not run when the value reaches the driver: its arguments
// are evaluated here, the call is not.
func TestEscapeInADeferStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tdefer lockIt(&q)\n"), narrowSeed, " FOR UPDATE")
}

// A `go` statement is the same, with a race on top: nothing orders the
// goroutine's write before the value is used.
func TestEscapeInAGoStatementStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tgo lockIt(&q)\n"), narrowSeed, " FOR UPDATE")
}

// An `if` without an `else` leaves the not-taken path real, and that path is
// exactly the world the fold emits.
func TestEscapeUnderAnIfWithoutElseStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tif c {\n\t\tlockIt(&q)\n\t}\n"), narrowSeed, " FOR UPDATE")
}

// A closure's body has not run where the literal sits; the fold's own rule for
// a NON-IIFE closure (unseenwrite.go, #72) is that the outside-appends world is
// the not-called path and emitting it is correct.
func TestEscapeInsideAClosureBodyStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tg := func() { lockIt(&q) }\n\t_ = g\n"), narrowSeed, " FOR UPDATE")
}

// An immediately-invoked literal whose body is NOT modellable is left opaque
// with the variable tracked (fold.go's contract). An escape inside it must not
// change that, because a `:=` inside a literal body binds in the CLOSURE's
// scope — a bare-name classifier reading through the boundary would untrack a
// variable of this scope that the literal never named.
func TestEscapeInsideAnIIFEBodyStillFolds(t *testing.T) {
	stillFolds(t, wrap("\tfunc() { lockIt(&q) }()\n"), narrowSeed, " FOR UPDATE")
}

// A `return` expression evaluates after every append, so the value handed out
// is precisely the one the fold assembled.
func TestEscapeInAReturnExpressionStillFolds(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + narrowSeed + "\"\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn use(q, &q)\n}\n"
	stillFolds(t, src, narrowSeed, " FOR UPDATE")
}

// The untrack is keyed on the NAME whose address is taken. `db.Scan(&row)` is
// in every query function in the corpus and must cost nothing.
func TestUnrelatedAddressDoesNotUntrack(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tvar row string\n" +
		"\tq := \"" + narrowSeed + "\"\n" +
		"\tsink(&row)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	stillFolds(t, src, narrowSeed, " FOR UPDATE")
}

// `&cfg.q` is the address of a FIELD. The tracked names are bare identifiers,
// so no field's address can be one of them — reading the identifier out of the
// selector instead of the operand would untrack a local that shares the field's
// name.
func TestStructFieldAddressDoesNotUntrackTheLocal(t *testing.T) {
	stillFolds(t, wrap("\tsink(&cfg.q)\n"), narrowSeed, " FOR UPDATE")
}

// The resolvable local alias, through a `var` binding rather than a `:=`. Every
// other mention of p is a dereference, so this block's own text decides what p
// names and #74's deref-write analysis governs it — reading through a pointer
// is not a write and must not untrack.
func TestVarDeclAliasReadStillFolds(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + narrowSeed + "\"\n" +
		"\tvar p = &q\n" +
		"\t_ = len(*p)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	stillFolds(t, src, narrowSeed, " FOR UPDATE")
}
