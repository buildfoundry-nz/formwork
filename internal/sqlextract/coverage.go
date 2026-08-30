package sqlextract

// coverage.go — the fold's untrack reasons, as data rather than as prose.
//
// This package refuses to track a variable in five distinct situations, and in
// each of them it emits NOTHING for that variable: no folded world at all. That
// is the deliberate answer to the #72/#73/#74/#310/#337/#314 class — a write the
// walk cannot see, dropped while the variable stays tracked, makes the fold emit
// a value no execution path produces — and its cost is a silence that reads
// exactly like a pass.
//
// So the silence has to be disclosed, and the disclosure has to be checkable.
// Three places have to agree about it and until #313 they were three
// independent copies in prose: this package's classifiers, the COVERAGE LIMIT
// block in internal/rules/sqlparse/locking.go that docs/reference.md tells
// adopters is "maintained as a checkable claim, not as prose", and the operator
// -facing #75 census. Two of those three had drifted — the block declared #72
// and #73 OPEN and unmodelled for a day after both landed, and named a
// behaviour ("a forward jump's skipped append is silently included") that is the
// opposite of what the shipped rule does.
//
// The list below is the single copy. Each reason is produced by the classifier
// that owns it — unfoldable stamps three, unmodelledWrites stamps two, and
// #311's three are stamped by builder.go and fold.go — so it cannot be edited
// into agreement with a comment; and
// internal/rules/sqlparse/locking_coverage_test.go requires every key here to
// appear in that block as a shape the REAL rule is measured going silent on.
// A reason added here with no disclosure fails; a disclosure whose behaviour
// changed fails; and an issue cited here that the block does not cite fails.
//
// #311 ADDED THE SECOND HALF OF THE JOB: a reason is now also a thing the fold
// EMITS, as a Site anchored at the construct that caused it (sites.go), so the
// disclosure an operator reads is produced by the run rather than looked up in
// a table beside it.

// UntrackReason is one reason the fold declines to read a composition. It is
// not an error and not a finding: it is the fold declining to answer, which a
// consumer must report as "not analysed" rather than let pass as "clean".
type UntrackReason struct {
	// Key is the stable machine-readable name. Consumers key on it, so it
	// changes only when the shape it names changes.
	Key string
	// Issue is the issue that named the shape — the one whose Reproduce section
	// holds the source that used to fabricate a world here.
	Issue string
	// Detail is the operator-facing phrase, rendered by the census inside
	// "could not be read (…)". A noun phrase naming the CONSTRUCT, because the
	// site is anchored at the construct and not at the query (sites.go): an
	// operator reading the line has to know what to go and look at.
	Detail string
	// Partial reports that the fold read the composition IN PART and emitted a
	// world from what it read, rather than emitting nothing.
	//
	// It is the difference between the two things a census line can mean, and
	// #311 is what happens when they are confused: a site carrying a
	// non-Partial reason claims the rule analysed nothing here, and putting one
	// on a composition the rule DID read is the false claim this arc closed.
	// A shape the COVERAGE LIMIT block discloses SILENT may only ever be
	// reported with a non-Partial reason, and a shape it discloses FIRES or
	// PASSES only with a Partial one.
	Partial bool
}

// The five UNTRACK reasons. Each is stamped by exactly one classifier, named in
// its comment, and each has a fixture in coverage_internal_test.go pinning that
// classifier to this value. Every one of them ends with the variable untracked:
// no world is emitted for it at all, which is why locking.go must disclose each
// as SILENT.
var (
	// reasonDerefWrite — unseenwrite.go's aliasUnsafe. The block writes through
	// a dereference somewhere and this name's address is taken, so a write to
	// it can arrive without any assignment to the identifier.
	reasonDerefWrite = UntrackReason{Key: "deref-write", Issue: "#74",
		Detail: "a write through a pointer taken from this query variable"}
	// reasonAddressEscape — escape.go's escapedNames. The address is handed
	// out — to a call, a store, a send, a composite literal — at a position
	// that provably runs, and what the far side does with it is not in this
	// file.
	reasonAddressEscape = UntrackReason{Key: "address-escape", Issue: "#310",
		Detail: "this query variable's address is handed out here"}
	// reasonCalledClosure — unseenwrite.go's closureWritten. A closure bound to
	// a name appends to this variable and is provably called, at any of the
	// binding and call spellings invoked.go recognises.
	reasonCalledClosure = UntrackReason{Key: "called-closure", Issue: "#72",
		Detail: "an append by a named closure that is called"}
	// reasonUnmodelledWrite — untrack.go's unmodelledWrites, assignment arm. A
	// statement form foldStmts does not fold — a switch or select arm, a loop
	// header or body, a bare block, an if/else where both arms write, a
	// labelled statement — assigns this name.
	reasonUnmodelledWrite = UntrackReason{Key: "unmodelled-write", Issue: "#314",
		Detail: "a write from a statement form the fold does not model"}
	// reasonRangeClause — untrack.go's unmodelledWrites, range arm. A
	// `for … = range` clause over a source that PROVABLY iterates overwrites
	// this name, so the pre-loop value is certainly gone.
	reasonRangeClause = UntrackReason{Key: "range-clause", Issue: "#314",
		Detail: "a `for … = range` clause that certainly overwrites it"}
)

// THE THREE REASONS THAT UNTRACK NOTHING, because there was never a tracked
// variable to untrack or because the fold keeps one it cannot fully read. They
// are unreadable compositions all the same, and #311 is about a channel that
// reported only what the rule DOES read: leaving these out would rebuild the
// same blindness one shape smaller.
//
// They are deliberately NOT in UntrackReasons. That list carries a stronger
// claim — locking_coverage_test.go requires every member to be disclosed
// SILENT — and only two of these three go silent at all.
var (
	// reasonStringsBuilder — builder.go. A builder composes through method
	// calls on a value that never holds a string-literal seed, so no name is
	// ever tracked and the fold has nothing to untrack: the query is invisible
	// to the whole mechanism rather than dropped by it.
	reasonStringsBuilder = UntrackReason{Key: "strings-builder", Issue: "#311",
		Detail: "a strings.Builder composition the fold never seeds"}
	// reasonDisqualifiedIIFE — fold.go's default arm, when iifeBody admits the
	// literal and iifeModellable refuses its body. The literal runs
	// unconditionally right there, and its appends are dropped while the
	// variable outside it stays TRACKED — so unlike every reason above, a world
	// IS emitted, assembled from the appends the walk happened to see
	// (locking.go's false-positive item 3).
	reasonDisqualifiedIIFE = UntrackReason{Key: "disqualified-iife", Issue: "#72",
		Detail:  "an append inside an immediately-invoked closure the fold cannot model",
		Partial: true}
	// reasonHeaderLiteral — sites.go's recordHeaderLiterals. A literal invoked in
	// a statement HEADER runs unconditionally on reaching the statement, and
	// arrives through a part no write-detection path inspects: foldStmts reads an
	// `if` condition for guards only, and untrackAssigned stops at *ast.FuncLit
	// everywhere. It drops its appends the same way (locking.go's item 4).
	//
	// EVERY HEADER, not just the `if` condition #72 named. `if func(){ q += … }();
	// b {`, `for func() bool { q += … }() {`, `switch func() int { q += … }() {`,
	// a `range` source and a `select`'s channel operands are the same literal,
	// provably run, in a different statement — and answering them one syntax form
	// at a time is how this class got here.
	reasonHeaderLiteral = UntrackReason{Key: "header-literal", Issue: "#72",
		Detail:  "an append inside a closure invoked in a statement header",
		Partial: true}
)

// UntrackReasons returns every reason the fold UNTRACKS a variable on, in a
// fixed order. Exported for the consumers that must disclose the fold's
// silence: locking.go's COVERAGE LIMIT block, which has to name each of these
// as a shape the real rule goes SILENT on.
func UntrackReasons() []UntrackReason {
	return []UntrackReason{
		reasonDerefWrite, reasonAddressEscape, reasonCalledClosure,
		reasonUnmodelledWrite, reasonRangeClause,
	}
}

// UnreadableReasons returns every reason a Site can carry — the five untrack
// reasons plus the three compositions the fold declines without untracking, in
// a fixed order.
//
// This is the list the operator-facing channel enumerates, and it is a superset
// of UntrackReasons rather than the same list because "no world was emitted"
// and "no world could be read" are different claims about the same file. An
// operator auditing a clean run needs the second one.
func UnreadableReasons() []UntrackReason {
	return append(UntrackReasons(),
		reasonStringsBuilder, reasonDisqualifiedIIFE, reasonHeaderLiteral)
}
