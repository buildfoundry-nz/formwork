package sqlparse_test

import "testing"

// #44 — the .go dedup key was (Line, Message), and Message is a single package
// constant, so the key was effectively LINE-ONLY. Any two genuinely distinct
// locking violations landing on one physical .go line collapsed to a single
// finding.
//
// This is an under-count, not a miss: the rule still fails the check, because at
// least one finding always survives. What is lost is the operator's ability to
// see that there are two things to fix — they fix the one that is reported, the
// gate stays red, and the second violation is discovered only by iteration.
//
// The discriminator is the seed COLUMN: two query variables seeded on one line
// are at different columns, while the seed/fold duplicate of ONE statement — the
// overlap the dedup exists for — shares both.
func TestTwoDistinctLockingQueriesOnOneLineBothReport(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q() (string, string) {\n" +
		"\ta, b := \"SELECT * FROM t WHERE s='x' FOR UPDATE\", \"SELECT * FROM u WHERE s='y' FOR UPDATE\"\n" +
		"\treturn a, b\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 2 {
		t.Fatalf("two distinct unordered locking SELECTs seeded on one line must "+
			"both report; got %d: %+v", len(ms), ms)
	}
}

// The control, and the reason the dedup exists at all: the expression walk and
// the assignment-fold pass can surface the SAME statement twice at the seed
// line. That duplicate must still collapse — otherwise fixing #44 doubles every
// folded finding.
func TestSeedAndFoldDuplicateStillCollapses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT * FROM t WHERE s='x' FOR UPDATE\"\n" +
		"\tq += \";\"\n" +
		"\treturn q\n}\n"
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("one statement surfaced by both the walk and the fold must report "+
			"once; got %d: %+v", len(ms), ms)
	}
}
