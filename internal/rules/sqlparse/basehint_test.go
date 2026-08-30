// basehint_test.go — #238.
//
// #42 is closed as a deliberate, thrice-disclosed trade: sqlextract emits a
// `base` world (no optional appends), and when two optional appends sit under
// complementary guards (`if a` / `if !a`) that world is one no path produces.
// If the ORDER BY lived in those branches, sql/locking-select-order fires on a
// query that is ordered on EVERY real path.
//
// The trade stands — removing `base` was tried across four review rounds and
// each attempt deleted a reachable world instead. What a developer gets is a
// deadlock-hazard violation on provably-correct code with nothing saying why or
// what to do. This adds the why.
//
// IT MUST NOT OVERCLAIM, which is the whole difficulty. `base` is frequently
// REACHABLE: a query observed between the two branches — `run(q)`, an early
// return, a db.Query — genuinely is `base` on a real path. So the hint says
// "may be", and it is attached only where a complementary pair actually exists.
// A hint on every finding carries no information, which is the state this is
// trying to leave.
package sqlparse_test

import (
	"strings"
	"testing"
)

const complementaryPairSrc = "package db\n\nfunc q(a bool) string {\n" +
	"\tq := \"SELECT id FROM appdb.projects WHERE id = ANY($1)\"\n" +
	"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
	"\tif !a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
	"\tq += \" FOR UPDATE\"\n" +
	"\treturn q\n}\n"

// No complementary pair anywhere: an ordinary unordered locking SELECT. The
// finding is a true positive and must carry NO hint.
const plainUnorderedSrc = "package db\n\nfunc q() string {\n" +
	"\tq := \"SELECT id FROM appdb.orders WHERE id = ANY($1)\"\n" +
	"\tq += \" FOR UPDATE\"\n" +
	"\treturn q\n}\n"

func TestLockingOrderExplainsTheDisclosedInfeasibleBaseWorld(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("commit.go", complementaryPairSrc))
	if len(ms) == 0 {
		t.Fatalf("precondition: the base world should still fire (#42 is a disclosed trade, not a fix)")
	}
	var hinted bool
	for _, m := range ms {
		if strings.Contains(m.Message, "#42") && strings.Contains(m.Message, "formwork:allow") {
			hinted = true
		}
	}
	if !hinted {
		t.Fatalf("a finding on the base world under a complementary pair must name the "+
			"possibility and the cure; got %+v", ms)
	}
}

// The narrowing, and the one that decides whether this is worth anything: a
// finding with no complementary pair behind it is an ordinary true positive and
// must not be softened by a hint that does not apply to it.
func TestLockingOrderDoesNotHintOnAnOrdinaryFinding(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("commit.go", plainUnorderedSrc))
	if len(ms) == 0 {
		t.Fatalf("precondition: an unordered locking SELECT must fire")
	}
	for _, m := range ms {
		if strings.Contains(m.Message, "#42") {
			t.Fatalf("no complementary pair exists here, so this is a true positive and "+
				"must carry no infeasible-base hint: %q", m.Message)
		}
	}
}

// The regression this change first introduced, pinned so it cannot come back.
// The dedup that collapses the walk/fold duplicate of ONE statement keys on
// (Line, Col, Message). Putting the note in the message at the emit site made
// the two copies differ, so they stopped collapsing and the operator saw the
// same violation twice. The note is applied AFTER dedup, and the risk flag is
// OR'd across the collapsed copies because only the fold's copy knows it came
// from a base world.
func TestTheInfeasibleBaseNoteDoesNotDefeatWalkFoldDedup(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("commit.go", complementaryPairSrc))
	if len(ms) != 1 {
		t.Fatalf("one statement must yield ONE finding; the note must not split the "+
			"walk/fold duplicate into two: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "#42") {
		t.Fatalf("the surviving finding must keep the note the collapsed copy carried: %q",
			ms[0].Message)
	}
}

// THE OR ACROSS COLLAPSED COPIES, which needs a fixture the obvious one does
// not give. The walk and the fold surface the same statement only in the shape
// the dedup comment names — a seed that ALREADY locks, plus an unconditional
// append, so the fold's base world differs from the seed and is emitted rather
// than skipped. Here the walk's copy (which knows nothing about guards) is seen
// FIRST, so dropping the duplicate without OR-ing its flag loses the note
// entirely. Verified: with the OR removed this fixture reports no note, while
// the plain complementary-pair fixture still does — which is why that one
// cannot pin this.
func TestTheNoteSurvivesWhenTheWalkCopyIsDedupedFirst(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM appdb.projects WHERE id = ANY($1) FOR UPDATE\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \";\"\n\treturn q\n}\n"
	ms := matches(t, c, file("commit.go", src))
	if len(ms) != 1 {
		t.Fatalf("the walk/fold duplicate must still collapse to one finding: %+v", ms)
	}
	if !strings.Contains(ms[0].Message, "#42") {
		t.Fatalf("the collapsed copy knew this was a base world under a complementary "+
			"pair; dropping the duplicate must not drop what it knew: %q", ms[0].Message)
	}
}

// THE PAIR REQUIREMENT, isolated. TestLockingOrderDoesNotHintOnAnOrdinaryFinding
// does not test it: its source has no optional appends at all, so base and full
// are the same text and a DIFFERENT guard (the occurrence count) suppresses the
// note. Deleting the pair requirement left that test green, which is a narrowing
// case that looks like it covers something and does not.
//
// This is the honest subject: ONE optional append, so a base world exists and
// fires, but no complementary pair — a=false is a real path, the finding is a
// true positive, and a note would be a lie about it.
func TestNoNoteOnABaseWorldThatIsSimplyReachable(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM appdb.projects WHERE id = ANY($1)\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	ms := matches(t, c, file("commit.go", src))
	if len(ms) == 0 {
		t.Fatalf("precondition: the a=false world is unordered and must fire")
	}
	for _, m := range ms {
		if strings.Contains(m.Message, "#42") {
			t.Fatalf("no complementary pair exists, so this base world is REACHABLE and "+
				"the finding is a true positive; a note would misdescribe it: %q", m.Message)
		}
	}
}

// THE BASE REQUIREMENT, isolated. A complementary pair exists here, but the
// world that fires is a BRANCH world (a=true → the lock without the order), not
// base — and a branch world is reachable by construction. Flagging every world
// of a paired variable rather than just base attaches the note here.
func TestNoNoteOnABranchWorldOfAPairedVariable(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM appdb.projects WHERE id = ANY($1)\"\n" +
		"\tif a {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" ORDER BY id FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	ms := matches(t, c, file("commit.go", src))
	if len(ms) == 0 {
		t.Fatalf("precondition: the a=true branch locks without ordering and must fire")
	}
	for _, m := range ms {
		if strings.Contains(m.Message, "#42") {
			t.Fatalf("this is a BRANCH world, reachable by construction — only base can "+
				"be the disclosed-infeasible one: %q", m.Message)
		}
	}
}
