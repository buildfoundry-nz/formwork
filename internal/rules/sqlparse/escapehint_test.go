// escapehint_test.go — #337, at the channel an operator actually reads.
//
// #337's headline is `run(add)`: a closure whose NAME is handed to a call
// appends the ORDER BY, the fold cannot follow the name into the callee, and the
// world it emits — the query without that append — fires as an unordered locking
// SELECT on code every real path orders.
//
// THAT BEHAVIOUR STAYS, and the reason is the whole difficulty. To a parse-only
// pass `run(add)` against `func run(f func()) { f() }` is the SAME TEXT as
// `register(add)` against `func register(f func()) {}`, where nothing ever calls
// the closure, the world without its appends IS the value, and the finding is
// the deadlock hazard this rule exists for. Untracking on the escape deletes
// that true positive to delete this false one — spec §10 measured that trade at
// ten findings for eight true ones — so the rule keeps firing on both, pinned by
// TestLockingClosureNameHandedToAHelperFires.
//
// What was missing is not detection. It is that the two findings were BYTE
// IDENTICAL: "locking SELECT over sibling rows has no deterministic ORDER BY
// (deadlock risk)", full stop, on program B and on `register(add)` and on an
// ordinary composition with no closure in it at all. The only place the
// difference was written down was a Go package comment in internal/sqlextract,
// and a disclosure an operator can only find by reading the analyzer's source is
// not a disclosure.
//
// So the finding carries a NOTE, on #42/#238's infeasibleBaseNote precedent one
// rule over: a disclosed false positive that is KEPT names the possibility and
// the cure at the point of use.
//
// IT IS A DECISION PROCEDURE, NEVER A VERDICT. The note cannot say "this is
// probably fine" — said on `register(add)` that is a lie about a real deadlock
// hazard, which is the trade this epic refuses, moved out of the rule and into
// the message. It names the closure and the callee, states what follows if the
// callee CALLS it and what follows if the callee only STORES it, and points at
// the `formwork:allow` marker for the first — a suppression `formwork lint`
// keeps enumerated, not a query rewritten to please the analyzer. It therefore
// attaches to `register(add)` too, and TestTheEscapeNoteSaysTheSameThingAboutTheTruePositive
// says so.
//
// Structure mirrors basehint_test.go, because the mechanism is the same one and
// its trap is the same trap: the note is applied AFTER the (Line, Col, Message)
// dedup and OR'd across the collapsed copies, since a message that differs
// between the walk copy and the fold copy stops them collapsing and the operator
// sees one violation twice.
package sqlparse_test

import (
	"strings"
	"testing"
)

// noteSrc wraps a body in a file declaring BOTH a helper that calls its
// argument and one that does not. They sit in one file on purpose: their
// indistinguishability to this pass is the decision the note hands over.
func noteSrc(params, body string) string {
	return "package db\n\nfunc run(f func()) { f() }\nfunc register(f func()) {}\n\n" +
		"func load(" + params + ") string {\n" + body + "\treturn q\n}\n"
}

const (
	noteSeed = "\tq := `SELECT id FROM t WHERE id = ANY($1)`\n"
	noteBind = noteSeed + "\tadd := func() { q += \" ORDER BY id\" }\n"
	noteLock = "\tq += \" FOR UPDATE\"\n"
)

// noteMessage runs one call spelling and returns the single finding's message.
// It fails rather than defaulting when the count is not one: "no finding" and "a
// finding with no note" are the two failures this file exists to tell apart.
func noteMessage(t *testing.T, call string) string {
	t.Helper()
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("commit.go", noteSrc("", noteBind+call+noteLock)))
	if len(ms) != 1 {
		t.Fatalf("precondition: the escaped closure's world must still fire — #337 is a "+
			"disclosed trade, not a fix: %+v", ms)
	}
	return ms[0].Message
}

// The disclosure itself, at both call sites. `run` invokes the closure and
// `register` does not; this pass cannot tell them apart, so both get the note
// and the operator gets the check to run.
func TestLockingOrderExplainsTheDisclosedClosureNameEscape(t *testing.T) {
	for _, tc := range []struct{ name, call, spelling string }{
		{"a helper that calls it", "\trun(add)\n", "run(add)"},
		{"a helper that does not", "\tregister(add)\n", "register(add)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := noteMessage(t, tc.call)
			for _, want := range []string{"#337", tc.spelling, "formwork:allow"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("an operator holding this finding has to be able to tell it "+
						"from a real hazard: the note must name the shape, the escape "+
						"(%q) and the cure; %q is missing from %q", tc.spelling, want, msg)
				}
			}
		})
	}
}

// THE NARROWING THAT DECIDES WHETHER THIS IS WORTH ANYTHING, and the one place
// this could re-make the mistake it exists to refuse. `register(add)` is a TRUE
// positive: nothing calls the closure, the unordered locking SELECT is the real
// value, and a note that softened it — "this is probably the #337 false
// positive" — would delete the hazard in the message instead of in the rule.
//
// So the two messages must be THE SAME TEXT apart from the callee spelling the
// operator is asked to go read. Anything that phrases the note as a verdict has
// to distinguish them, and this pass cannot.
func TestTheEscapeNoteSaysTheSameThingAboutTheTruePositive(t *testing.T) {
	viaRun := noteMessage(t, "\trun(add)\n")
	viaRegister := noteMessage(t, "\tregister(add)\n")
	if got := strings.Replace(viaRegister, "register(add)", "run(add)", 1); got != viaRun {
		t.Fatalf("register(add) is a TRUE positive and run(add) is the disclosed false "+
			"one, and to this pass they are one program: the note must say the same "+
			"thing about both, differing only in the callee it names.\n run(add): %q\n"+
			" register(add): %q", viaRun, viaRegister)
	}
	// The identity above cannot catch the failure that matters on its own: a note
	// reading "this is the disclosed false positive" is equally identical across
	// the two and equally a lie about `register(add)`. So the note must state
	// BOTH directions and name the second one as the hazard — which is what
	// makes it a decision procedure instead of a verdict.
	for _, want := range []string{"CALLS", "STORES", "deadlock hazard"} {
		if !strings.Contains(viaRegister, want) {
			t.Fatalf("the note has to say what follows if the callee runs the closure AND "+
				"what follows if it only stores it, or it is a verdict on a shape this "+
				"pass cannot decide; %q is missing from %q", want, viaRegister)
		}
	}
}

// The other direction, first half. A note on every finding carries no
// information, which is the state this is trying to leave.
func TestNoEscapeNoteOnAnOrdinaryFinding(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := noteSrc("", noteSeed+noteLock)
	ms := matches(t, c, file("commit.go", src))
	if len(ms) == 0 {
		t.Fatalf("precondition: an unordered locking SELECT must fire")
	}
	for _, m := range ms {
		if strings.Contains(m.Message, "#337") {
			t.Fatalf("no closure escapes here, so this is an ordinary true positive and "+
				"must carry no escape note: %q", m.Message)
		}
	}
}

// The other direction, second half — and the case the row above cannot cover.
// This finding IS a disclosed false positive, of the OTHER kind: the #42 base
// world under a complementary pair. It carries that note and must not acquire
// this one, or the escape note becomes decoration attached to whatever else was
// already unusual about a finding.
func TestNoEscapeNoteOnABaseWorldFindingWithNoEscape(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := noteSrc("a bool", noteSeed+
		"\tif a {\n\t\tq += \" LIMIT 5\"\n\t}\n\tif !a {\n\t\tq += \" LIMIT 5\"\n\t}\n"+
		noteLock)
	ms := matches(t, c, file("commit.go", src))
	if len(ms) != 1 {
		t.Fatalf("precondition: the base world under a complementary pair fires as one "+
			"finding: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "#42") {
		t.Fatalf("precondition: this is the #42 world and must carry ITS note: %q",
			ms[0].Message)
	}
	if strings.Contains(ms[0].Message, "#337") {
		t.Fatalf("no closure escapes here; a note attached to every unusual finding "+
			"tells an operator nothing about which shape they are holding: %q",
			ms[0].Message)
	}
}

// DEDUP, ORDER ONE: the flagged copy is the one DROPPED, and a second query in
// the same file makes the parallel slice's alignment load-bearing.
//
// The seed here ALREADY locks, so the expression walk emits a finding at the
// seed position — carrying no escape flag, because only the fold knows about
// closures — and the fold's world (the same statement plus ";") collapses onto
// it. Dropping that duplicate without OR-ing its flag forward loses the note
// entirely, which is the #238 regression in this feature's own shape.
//
// `p` is the other half of the assertion, not scenery. It fires too, from a
// composition with no closure in it, and it is emitted BETWEEN the two copies of
// `q` — so a parallel escape slice that is not compacted alongside ms lands the
// note on the wrong finding or on none. Measured: with the compaction omitted,
// the finding that earned the note reports the bare message.
func TestTheEscapeNoteSurvivesWhenTheFlaggedCopyIsDeduped(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc run(f func()) { f() }\n\nfunc load() string {\n" +
		"\tq := `SELECT id FROM t WHERE id = ANY($1) FOR UPDATE`\n" +
		"\tadd := func() { q += \" ORDER BY id\" }\n" +
		"\trun(add)\n\tq += \";\"\n" +
		"\tp := `SELECT id FROM u WHERE id = ANY($1)`\n" +
		"\tp += \" FOR UPDATE\"\n\treturn q + p\n}\n"
	ms := matches(t, c, file("commit.go", src))
	if len(ms) != 2 {
		t.Fatalf("two distinct queries, and the walk/fold duplicate of the first must "+
			"still collapse to one finding: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "#337") {
		t.Fatalf("the collapsed copy knew a closure's name had escaped; dropping the "+
			"duplicate must not drop what it knew: %q", ms[0].Message)
	}
	if strings.Contains(ms[1].Message, "#337") {
		t.Fatalf("no closure touches the second query — the note has landed on the "+
			"wrong finding, which is what an unaligned parallel slice does: %q",
			ms[1].Message)
	}
}

// DEDUP, ORDER TWO: the flagged copy is the one KEPT. Two folded worlds of one
// statement — with and without the optional LIMIT — differ in text, share
// (Line, Col, Message), and collapse. Putting the note in the message at the
// emit site is what the #238 comment forbids; here it would still collapse,
// which is exactly why this case is not the whole story and the row above
// exists.
func TestTheEscapeNoteSurvivesWhenTheFlaggedCopyIsKept(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := noteSrc("a bool", noteBind+"\trun(add)\n"+
		"\tif a {\n\t\tq += \" LIMIT 5\"\n\t}\n"+noteLock)
	ms := matches(t, c, file("commit.go", src))
	if len(ms) != 1 {
		t.Fatalf("one statement must yield ONE finding; the note must not split the "+
			"two folded worlds into two: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "run(add)") {
		t.Fatalf("the surviving copy is the flagged one and must keep its note: %q",
			ms[0].Message)
	}
}

// BOTH NOTES ON ONE FINDING, and stable. A complementary pair AND an escaped
// closure: three folded worlds collapse into one finding, the survivor is the
// `full` world (which is not base), and the #42 flag has to be OR'd forward from
// a dropped copy while the #337 flag rides on every copy.
//
// The order is asserted, not just the presence. Two notes appended by two
// independent passes over the same slice have exactly one honest order, and a
// message whose halves swap between runs is a message nobody can grep for.
func TestAFindingCanCarryBothNotes(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := noteSrc("a bool", noteBind+"\trun(add)\n"+
		"\tif a {\n\t\tq += \" LIMIT 5\"\n\t}\n\tif !a {\n\t\tq += \" LIMIT 5\"\n\t}\n"+
		noteLock)
	ms := matches(t, c, file("commit.go", src))
	if len(ms) != 1 {
		t.Fatalf("three folded worlds of one statement must collapse to one finding: %+v", ms)
	}
	base := strings.Index(ms[0].Message, "#42")
	esc := strings.Index(ms[0].Message, "#337")
	if base < 0 || esc < 0 {
		t.Fatalf("this world is BOTH the #42 base world and the world an escaped "+
			"closure's appends are missing from; an operator needs both checks: %q",
			ms[0].Message)
	}
	if base > esc {
		t.Fatalf("the two notes must always append in the same order — #42 then #337 — "+
			"or the message is not one an operator can recognise twice: %q", ms[0].Message)
	}
}

// THE DISCLOSURE AND THE MESSAGE, CHECKED AGAINST EACH OTHER — the census_wiring
// precedent, which fails in BOTH directions and is why closing a gap while the
// docs still described it open broke the build.
//
// docs/reference.md sends adopters to the COVERAGE LIMIT block as "the current
// list". A shape whose line there promises the finding carries a NOTE and whose
// findings carry none is a promise an operator relies on and does not get; a
// shape whose findings carry a note the block never mentions is the disclosure
// gap #337 was filed about, re-created one shape over. Neither can be edited
// into agreement one file at a time.
func TestEveryNotePromisedIsAttachedAndEveryNoteAttachedIsPromised(t *testing.T) {
	block := coverageBlock(t)
	idx := covShapeRE.FindAllStringSubmatchIndex(block, -1)
	if len(idx) == 0 {
		t.Fatal("no SHAPE lines in the COVERAGE LIMIT block — this test would pass " +
			"over nothing")
	}
	promised := 0
	for i, m := range idx {
		end := len(block)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		key := block[m[2]:m[3]]
		claimed := strings.Contains(shapeEntry(block[m[0]:end]), "carries a NOTE")
		if claimed {
			promised++
		}
		src, ok := covShapes[key]
		if !ok {
			// A shape with no composition behind it is
			// TestCoverageLimitDisclosesWhatTheRuleDoes' failure, not this one's.
			continue
		}
		c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
		noted := false
		for _, ms := range matches(t, c, file("q.go", src)) {
			if strings.Contains(ms.Message, " — NOTE:") {
				noted = true
			}
		}
		switch {
		case claimed && !noted:
			t.Errorf("shape %q: the block promises the finding carries a NOTE and no "+
				"finding on it carries one — the manual points adopters at that line", key)
		case !claimed && noted:
			t.Errorf("shape %q: findings on it carry a NOTE the block never mentions — "+
				"a disclosure that lives only in the message is invisible to anyone "+
				"reading the current list", key)
		}
	}
	if promised == 0 {
		t.Fatal("no SHAPE line in the block promises a NOTE, so the first half of this " +
			"test asserted nothing")
	}
}

// shapeEntry trims a SHAPE line and its continuation lines out of the run of
// block text that follows it. Every entry in the table is a `//\t`-indented run;
// the prose after the table is not, and the LAST entry would otherwise swallow
// all of it. Measured: without this, `header-literal` inherited item 5's
// sentence about the #337 note and was reported as promising a note it never
// carries — the test failing on the wrong shape, which is the same defect it
// exists to catch, one layer up.
func shapeEntry(section string) string {
	lines := strings.Split(section, "\n")
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "//\t") {
			return strings.Join(lines[:i], "\n")
		}
	}
	return section
}
