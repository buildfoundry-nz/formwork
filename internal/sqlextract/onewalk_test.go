// onewalk_test.go — #45.
//
// FromGoReassembled walked every .go AST TWICE: once to collect reassembled
// expression candidates, then again inside foldAssignments to find function and
// function-literal bodies for the #36 assignment-flow fold.
//
// This is a refactor, so there is no red-then-green: the output must be
// byte-identical. What replaces the red step is a CHARACTERIZATION test — one
// that pins the behaviour a naive merge silently breaks, passes before the
// change, and must still pass after.
//
// THE TRAP, measured before writing anything. The collector returns false when
// it consumes an expression, so it does NOT descend into it; the fold walk
// descended everything independently. A function literal inside a CONSUMED
// expression is therefore reached only by the second walk:
//
//	fmt.Sprintf("SELECT %s FROM t", func() string {
//	        c := "SELECT id FROM inner WHERE s = 'y'"
//	        c += " FOR UPDATE"            // <- found ONLY by the fold walk
//	        return c
//	}())
//
// The Sprintf IS consumed (it yields "SELECT fw_expr FROM t"), so the obvious
// one-walk merge — keep the `return false` and fold on the way past — drops that
// inner locking SELECT entirely. That is a false negative on a deadlock hazard,
// the direction this package exists to avoid.
package sqlextract_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

const funcLitInsideConsumed = "package db\n\nimport \"fmt\"\n\nfunc q() string {\n" +
	"\treturn fmt.Sprintf(\"SELECT %s FROM t\", func() string {\n" +
	"\t\tc := \"SELECT id FROM inner WHERE s = 'y'\"\n" +
	"\t\tc += \" FOR UPDATE\"\n" +
	"\t\treturn c\n\t}())\n}\n"

func TestFoldReachesAFuncLitInsideAConsumedExpression(t *testing.T) {
	got, _, err := sqlextract.FromGoReassembled("x.go", []byte(funcLitInsideConsumed))
	if err != nil {
		t.Fatal(err)
	}
	var sawOuter, sawInner bool
	for _, c := range got {
		if strings.Contains(c.Text, "SELECT fw_expr FROM t") {
			sawOuter = true
		}
		if c.Text == "SELECT id FROM inner WHERE s = 'y' FOR UPDATE" {
			sawInner = true
		}
	}
	if !sawOuter {
		t.Errorf("the Sprintf must still be consumed as one candidate; got %v", got)
	}
	if !sawInner {
		t.Errorf("the fold must still reach a func literal inside a CONSUMED expression — " +
			"dropping it is a silent miss on a locking SELECT")
	}
}

// Order is consumed downstream (dedup, finding sort), so the merge must not
// interleave. Every expression candidate precedes every fold candidate, which is
// what two sequential walks produced and what one walk into two slices preserves.
func TestExpressionCandidatesStillPrecedeFoldCandidates(t *testing.T) {
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\t_ = \"SELECT lit FROM t\"\n" +
		"\tq := \"SELECT folded FROM t\"\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\t_ = \"SELECT lit2 FROM t\"\n" +
		"\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	lastExpr, firstFold := -1, -1
	for i, c := range got {
		switch {
		case strings.HasPrefix(c.Text, "SELECT lit"):
			lastExpr = i
		// The FULL folded text, not a prefix: the seed literal and the appended
		// fragment are each their own EXPRESSION candidate, so a Contains match
		// on "SELECT folded" classifies an expression candidate as the fold and
		// the test fails against correct output.
		case c.Text == "SELECT folded FROM t FOR UPDATE" && firstFold < 0:
			firstFold = i
		}
	}
	if lastExpr < 0 || firstFold < 0 {
		t.Fatalf("precondition: expected both kinds of candidate; got %v", got)
	}
	if firstFold < lastExpr {
		t.Fatalf("a fold candidate at %d precedes an expression candidate at %d — "+
			"the merged walk interleaved them: %v", firstFold, lastExpr, got)
	}
}

// A nested expression inside a consumed one must NOT become its own candidate.
// The old walk got this from `return false`; a merge that keeps descending has
// to suppress it explicitly, and forgetting to is how a refactor quietly doubles
// the parse load.
func TestNestedExpressionInsideAConsumedOneIsNotItsOwnCandidate(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\treturn \"SELECT a \" + \"FROM t \" + \"WHERE s = 'x'\"\n}\n"
	got, _, err := sqlextract.FromGoReassembled("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("a consumed + chain is ONE candidate, not its parts; got %d: %v", len(got), got)
	}
}
