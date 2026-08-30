// fold_closure_name_escape_test.go — #337's residue, NOTICED.
//
// `run(add)` is the issue's own failure scenario, and it still folds. That is
// the decision, not an oversight: a parse-only pass cannot tell
// `func run(f func()) { f() }` from `func register(f func()) {}`, so untracking
// on the escape silences the register() case, where the closure never runs, the
// world built without its appends IS the value, and the finding on it is the
// unordered locking SELECT this rule exists for. Spec §10 measured that trade at
// ten findings for eight true ones. TestClosureNameHandedToAHelperKeepsFolding
// and TestLockingClosureNameHandedToAHelperFires pin the behaviour green.
//
// The issue's own premise is what this file spends the other way: NOTICING the
// escape is free, ACTING on it is what costs. So the escape is noticed, and the
// notice is spent on telling the operator holding the finding which shape they
// are looking at — a disclosure they can only find by reading Go source is not a
// disclosure.
//
// TWO DIRECTIONS, both pinned here, because either one silently reverses the
// decision:
//
//   - the flag must be SET on the shape, or the disclosure is not delivered and
//     an operator cannot tell program B from a real hazard;
//   - the flag must be ABSENT everywhere else, because a marker on every finding
//     carries no information;
//   - and the CANDIDATE SET must not move. Noticing an escape must not untrack
//     the query, add a world or drop one. That is what
//     TestNoticingTheEscapeEmitsExactlyWhatItEmittedBefore holds, against
//     candidate text/line/col recorded from the tree before the detector
//     existed.
package sqlextract_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

const nameEscapeSeed = "SELECT id FROM t WHERE s = 'x'"

const (
	nameEscapeSeedLine = "\tq := `SELECT id FROM t WHERE s = 'x'`\n"
	nameEscapeBind     = nameEscapeSeedLine + "\tadd := func() { q += \" ORDER BY id\" }\n"
	nameEscapeTail     = "\tq += \" FOR UPDATE\"\n"
)

// nameEscapeCands reassembles one block and returns every candidate it emits,
// in order. The harness declares a helper that CALLS its argument and one that
// does not, in one file, because their indistinguishability is the whole reason
// the fold keeps folding here; and a method callee and a slice-element callee,
// because the rendering handed to the operator has to name a callee it cannot
// resolve without pretending it resolved one.
func nameEscapeCands(t *testing.T, body string) []sqlextract.Candidate {
	t.Helper()
	src := "package db\n\nfunc run(f func()) { f() }\nfunc register(f func()) {}\n" +
		"func withTx(f func(tx int)) { f(0) }\n\ntype hooks struct{}\n\n" +
		"func (hooks) run(f func()) { f() }\n\n" +
		"func load(h hooks, fs []func(func())) string {\n" + body + "\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	return got
}

// nameEscapeRisk is the flag on the one world the fold builds on the seed.
// It fails rather than defaulting when that world is missing, because "no
// candidate" and "an unflagged candidate" are the two failures this file exists
// to tell apart.
func nameEscapeRisk(t *testing.T, body string) string {
	t.Helper()
	var worlds []sqlextract.Candidate
	for _, c := range nameEscapeCands(t, body) {
		if c.Text != nameEscapeSeed && strings.HasPrefix(c.Text, nameEscapeSeed) {
			worlds = append(worlds, c)
		}
	}
	if len(worlds) != 1 {
		t.Fatalf("want exactly one fold world built on the seed, got %d: %v", len(worlds), worlds)
	}
	return worlds[0].ClosureEscapeRisk
}

// Every shape below hands a closure that appends to q somewhere this pass cannot
// follow, and every one of them keeps folding. The flag is the only thing that
// separates the emitted world from a world nothing is wrong with.
//
// The inline spellings are here for the reason the named ones are: `run(add)`
// and `run(func(){ … })` are one program written two ways, fold_iife_test.go
// pins the anonymous one green as a LOAD-BEARING GUARD, and leaving it unflagged
// would make it the second undisclosed false positive of the shape this work
// exists to close.
var closureNameEscapeShapes = []struct{ name, body, want string }{
	{
		name: "a helper that calls it",
		body: nameEscapeBind + "\trun(add)\n" + nameEscapeTail,
		want: "run(add)",
	},
	{
		name: "a helper that does not",
		body: nameEscapeBind + "\tregister(add)\n" + nameEscapeTail,
		want: "register(add)",
	},
	{
		name: "an inline literal",
		body: nameEscapeSeedLine + "\trun(func() { q += \" ORDER BY id\" })\n" + nameEscapeTail,
		want: "run(func(){…})",
	},
	{
		name: "an inline literal with parameters",
		body: nameEscapeSeedLine + "\twithTx(func(tx int) { q += \" ORDER BY id\" })\n" + nameEscapeTail,
		want: "withTx(func(…){…})",
	},
	{
		name: "a method callee",
		body: nameEscapeBind + "\th.run(add)\n" + nameEscapeTail,
		want: "h.run(add)",
	},
	{
		// The callee is an element, so no name this pass resolves says which
		// function runs. The rendering says exactly that rather than inventing
		// a name: the operator still gets the argument, which is the half that
		// identifies the closure.
		name: "a callee no name resolves",
		body: nameEscapeBind + "\tfs[0](add)\n" + nameEscapeTail,
		want: "…(add)",
	},
	{
		// Two escapes, one variable. The EARLIEST wins, so which spelling an
		// operator is shown cannot change with map iteration order.
		name: "the earliest of two escapes",
		body: nameEscapeBind + "\trun(add)\n\tregister(add)\n" + nameEscapeTail,
		want: "run(add)",
	},
}

func TestEscapedClosureIsDisclosedOnTheWorldThatDropsIt(t *testing.T) {
	for _, tc := range closureNameEscapeShapes {
		t.Run(tc.name, func(t *testing.T) {
			if got := nameEscapeRisk(t, tc.body); got != tc.want {
				t.Fatalf("the fold emits a world this closure's appends are missing from, "+
					"and an operator holding the finding has to be able to recognise it: "+
					"risk = %q, want %q", got, tc.want)
			}
		})
	}
}

// The other direction. A marker on every finding carries no information, so
// every shape here must carry none — and each is a DIFFERENT reason for that:
//
//   - `add()` is provably called, so #72 untracks q and no world is emitted at
//     all. There is nothing to flag and nothing to disclose.
//   - `_ = add` hands the name to no call. The closure never runs, the emitted
//     world is the real one, and a finding on it is a true positive.
//   - an escape that touches another variable says nothing about q.
//   - a composition with no closure in it is the ordinary case, and it is most
//     of the corpus.
//
// worlds IS PART OF THE ASSERTION, not description, and the `add()` row is why
// it exists. Measured: stamping a constant escape spelling on every emitted
// candidate reddens the other three rows and leaves that one green, because a
// scope that emits no world has nothing to carry a wrong flag. Checking only
// the flag would make the one row whose whole content is "this emits nothing"
// the one row that could never fail. So each row also states how many worlds
// the seed produces, and reversing #72's untrack reddens the `add()` row on
// that half.
func TestNoEscapeMeansNoDisclosure(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		worlds     int
	}{
		{"a provable call, so the query is untracked", nameEscapeBind + "\tadd()\n" + nameEscapeTail, 0},
		{"no call at all", nameEscapeBind + "\t_ = add\n" + nameEscapeTail, 1},
		{
			"an escape that touches another variable",
			nameEscapeSeedLine + "\tp := \"x\"\n\tbump := func() { p += \"y\" }\n" +
				"\trun(bump)\n\t_ = p\n" + nameEscapeTail, 1,
		},
		{"an ordinary composition", nameEscapeSeedLine + nameEscapeTail, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worlds := 0
			for _, c := range nameEscapeCands(t, tc.body) {
				if c.ClosureEscapeRisk != "" {
					t.Fatalf("nothing escaped here, so %q must carry no escape "+
						"disclosure: got %q", c.Text, c.ClosureEscapeRisk)
				}
				if c.Text != nameEscapeSeed && strings.HasPrefix(c.Text, nameEscapeSeed) {
					worlds++
				}
			}
			if worlds != tc.worlds {
				t.Fatalf("this shape builds %d world(s) on the seed, not %d — "+
					"a row asserting only that nothing is flagged cannot fail on a "+
					"shape that emits nothing", tc.worlds, worlds)
			}
		})
	}
}

// THE LOAD-BEARING PROPERTY OF THIS WORK: noticing an escape changes nothing
// about WHAT is emitted.
//
// Untracking on the escape is the fix #337 proposes and the one this repo has
// twice measured and refused. It would show up here as a world disappearing.
// Widening the fold to model the closure would show up as a world appearing or
// a text changing. Both are invisible to the assertions above — a missing
// candidate carries no flag either — so the emitted set is compared against
// text/line/col recorded from the tree BEFORE the detector existed, with the new
// field deliberately not rendered.
//
// It fails in both directions, which is the point: silencing the false positive
// reddens it, and so does anything that quietly adds a world to make the
// disclosure look better.
func TestNoticingTheEscapeEmitsExactlyWhatItEmittedBefore(t *testing.T) {
	for _, tc := range closureNameEscapeShapes {
		t.Run(tc.name, func(t *testing.T) {
			want, ok := candidatesBeforeTheDetector[tc.name]
			if !ok {
				t.Fatalf("no recorded pre-detector emission for %q — a shape whose "+
					"emission nothing recorded cannot be shown to be unchanged", tc.name)
			}
			got := renderCands(nameEscapeCands(t, tc.body))
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("noticing the escape must not change WHICH candidates are "+
					"emitted:\n got: %s\nwant: %s", strings.Join(got, "\n      "),
					strings.Join(want, "\n      "))
			}
		})
	}
}

// renderCands prints every field of a candidate EXCEPT ClosureEscapeRisk, so the
// comparison above is over the emission as it stood before the field existed.
func renderCands(cands []sqlextract.Candidate) []string {
	out := []string{}
	for _, c := range cands {
		out = append(out, fmt.Sprintf("L%d C%d %q partial=%v base=%v",
			c.Line, c.Col, c.Text, c.Partial, c.InfeasibleBaseRisk))
	}
	return out
}

// Recorded at 4a13b75a, before invoked.go could see an escape at all.
var candidatesBeforeTheDetector = map[string][]string{
	"a helper that calls it": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C23 \" ORDER BY id\" partial=false base=false",
		"L15 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
	"a helper that does not": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C23 \" ORDER BY id\" partial=false base=false",
		"L15 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
	"an inline literal": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C20 \" ORDER BY id\" partial=false base=false",
		"L14 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
	"an inline literal with parameters": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C29 \" ORDER BY id\" partial=false base=false",
		"L14 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
	"a method callee": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C23 \" ORDER BY id\" partial=false base=false",
		"L15 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
	"a callee no name resolves": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C23 \" ORDER BY id\" partial=false base=false",
		"L15 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
	"the earliest of two escapes": {
		"L12 C7 \"SELECT id FROM t WHERE s = 'x'\" partial=false base=false",
		"L13 C23 \" ORDER BY id\" partial=false base=false",
		"L16 C7 \" FOR UPDATE\" partial=false base=false",
		"L12 C7 \"SELECT id FROM t WHERE s = 'x' FOR UPDATE\" partial=false base=false",
	},
}
