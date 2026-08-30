// fold_escape_test.go — #310, the half of #74 that was never built.
//
// #74's Scope says verbatim: "In: writes through `*p` where `p` was taken from
// a tracked variable, AND THE ADDRESS HANDED TO A HELPER". Only the first half
// shipped. aliasUnsafe returns nil unless the SAME block also writes through a
// dereference, so `lockIt(&q)` — the ordinary spelling, and the one the spec
// itself calls the canonical builder — leaves q fully tracked while the
// helper's write is invisible. The fold then emits a value assembled from only
// the writes it happened to see, which §4.1 promises never to emit.
//
// It breaks in both directions. With the helper adding the ORDER BY the fold
// reports an unordered locking SELECT no path holds (a false positive on
// provably safe code, in a gate that forbids `formwork:allow` escapes). With
// the helper adding the LOCK a real deadlock hazard goes unreported.
//
// The fix classifies every `&name` in a scope: a RESOLVABLE LOCAL ALIAS
// (`p := &q` bound at the top level of the block, every other mention of p a
// dereference) is left to #74's deref-write analysis, and every other `&name`
// is an ESCAPE that untracks the name — but only where the escape provably
// runs. The narrowing lives in fold_escape_narrowing_test.go; this file is the
// escapes.
package sqlextract_test

import "testing"

// noFoldWorld asserts the fold emits NO value built on seed — the variable was
// untracked, so nothing is claimed about a query whose writes are out of reach.
// The bare seed literal the expression walk emits is not a fold world and is
// deliberately not asserted about here.
func noFoldWorld(t *testing.T, src, seed string) {
	t.Helper()
	if got := foldOnly(foldTexts(t, src), seed); len(got) != 0 {
		t.Fatalf("the address escapes this block, so a world assembled from only "+
			"the visible appends exists on no path; emitted %q", got)
	}
}

const escSeed = "SELECT id FROM t WHERE s = 'x'"

// The scenario #310 reports, in the SILENT direction: the helper adds the lock,
// so the real value on every path is a locking SELECT with no ORDER BY.
func TestHelperGivenAddressDoesNotFabricateAWorld(t *testing.T) {
	src := "package db\n\nfunc lockIt(p *string) { *p += \" FOR UPDATE\" }\n\n" +
		"func f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tq += \" AND y = 1\"\n" +
		"\tlockIt(&q)\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// The LOUD direction: the helper adds the ORDER BY, the caller adds the lock.
// The fabricated world is an unordered locking SELECT the code never produces.
func TestHelperGivenAddressDoesNotFabricateAnUnorderedWorld(t *testing.T) {
	src := "package db\n\nfunc orderIt(p *string) { *p += \" ORDER BY id\" }\n\n" +
		"func f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\torderIt(&q)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// No writing helper in the file at all: the callee is another package's, so
// nothing about it is even readable here.
func TestCrossPackageCallGivenAddressUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tsqlhelp.OrderBy(&q)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// A method on a receiver, the shape a query builder actually takes.
func TestMethodCallGivenAddressUntracks(t *testing.T) {
	src := "package db\n\nfunc f(b B) string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tb.Decorate(&q)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// Stored in a local and never dereferenced: `r` is not a resolvable alias, so
// `&q` is an escape even though no call takes it directly.
func TestAddressStoredInALocalUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tr := &q\n" +
		"\tsink(r)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// The same through a `var` declaration rather than a `:=`.
func TestAddressStoredInAVarDeclUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tvar r = &q\n" +
		"\tsink(r)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

func TestAddressInACompositeLiteralUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tsink([]*string{&q})\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

func TestAddressSentOnAChannelUntracks(t *testing.T) {
	src := "package db\n\nfunc f(ch chan *string) string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tch <- &q\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

func TestAddressStoredInAStructFieldUntracks(t *testing.T) {
	src := "package db\n\nfunc f(x *holder) string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tx.p = &q\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

func TestAddressStoredInAMapUntracks(t *testing.T) {
	src := "package db\n\nfunc f(m map[string]*string) string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tm[\"k\"] = &q\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// Pointer to pointer: `p` IS only ever dereferenced except for the one place
// its own address is taken, and that one place hands q's address on twice over.
func TestPointerToPointerUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tp := &q\n" +
		"\tsink(&p)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// An `if` Init runs before the condition is tested, on every path that reaches
// the statement — so an escape there provably runs even though the branch may
// not be taken.
func TestEscapeInAnIfInitUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tif ok := check(&q); ok {\n\t\t_ = ok\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// The condition itself is evaluated on every path that reaches the statement.
func TestEscapeInAnIfConditionUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tif check(&q) {\n\t\t_ = 1\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// A label does not change whether the statement under it runs.
func TestEscapeUnderALabelUntracks(t *testing.T) {
	src := "package db\n\nfunc f() string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"L:\n\tlockIt(&q)\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}

// The LEFT side of an assignment is evaluated too, and an index or a selector
// on the target can carry a call that takes the address. `foldAssign` returns
// early on an index target — it cannot be a tracked name — so nothing else in
// the walk looks at this statement and q stays tracked through it.
func TestEscapeOnAnAssignmentTargetUntracks(t *testing.T) {
	src := "package db\n\nfunc f(m map[string]int) string {\n" +
		"\tq := \"" + escSeed + "\"\n" +
		"\tm[key(&q)] = 1\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	noFoldWorld(t, src, escSeed)
}
