// fold_goto_test.go — #73.
//
// `goto` was modelled nowhere: foldStmts walks a statement list linearly, so a
// forward jump's skipped appends were folded in as though control fell through.
// The jumped-over path had no world at all.
//
// That is the silent direction. With the skipped append supplying the ORDER BY,
// the goto path is an unordered locking SELECT — a real deadlock hazard — and
// sql/locking-select-order reported nothing, because the only world emitted was
// the fall-through one, which is ordered.
//
// A forward goto is structurally an OPTIONAL append: the statements between the
// jump and its label run on one path and not the other. The fold already models
// that, and always emits the world without the optional appends, so expressing
// it that way supplies the missing world through machinery that already exists
// rather than a second mechanism to keep in step.
package sqlextract_test

import (
	"strings"
	"testing"
)

// The hazard: on the `b` path the ORDER BY is jumped over, leaving a locking
// SELECT with no ordering. Some emitted world must model it.
func TestForwardGotoEmitsTheJumpedOverWorld(t *testing.T) {
	src := "package db\n\nfunc q(b bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tif b {\n\t\tgoto lock\n\t}\n" +
		"\tq += \" ORDER BY id\"\n" +
		"lock:\n\t_ = b\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"

	seed := "SELECT id FROM t WHERE s = 'x'"
	worlds := foldOnly(foldTexts(t, src), seed)

	var sawJumped bool
	for _, w := range worlds {
		if strings.Contains(w, "FOR UPDATE") && !strings.Contains(w, "ORDER BY") {
			sawJumped = true
		}
	}
	if !sawJumped {
		t.Fatalf("the goto path is an unordered locking SELECT and no world models "+
			"it; emitted: %q", worlds)
	}
}

// The fall-through path must survive too. Emitting only the jumped-over world
// would trade one silent miss for another.
func TestForwardGotoKeepsTheFallThroughWorld(t *testing.T) {
	src := "package db\n\nfunc q(b bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tif b {\n\t\tgoto lock\n\t}\n" +
		"\tq += \" ORDER BY id\"\n" +
		"lock:\n\t_ = b\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"

	seed := "SELECT id FROM t WHERE s = 'x'"
	if !hasFoldText(foldTexts(t, src), seed+" ORDER BY id FOR UPDATE") {
		t.Fatalf("the fall-through world must still be emitted; got %q",
			foldOnly(foldTexts(t, src), seed))
	}
}

// The narrowing. A label with no goto jumping over anything must not make
// ordinary appends optional — that would emit a world for a path the code does
// not have, which is the defect this whole package is about, pointed the other
// way.
func TestLabelWithoutAForwardGotoDoesNotMakeAppendsOptional(t *testing.T) {
	src := "package db\n\nfunc q() string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"top:\n\t_ = 1\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"

	seed := "SELECT id FROM t"
	for _, w := range foldOnly(foldTexts(t, src), seed) {
		if w == seed {
			t.Fatalf("a label nothing jumps over must not make the append optional; "+
				"emitted the seed as a world: %q", w)
		}
	}
}

// A BACKWARD goto is a loop: the appends between the label and the branch repeat
// an unknown number of times, and the fold has no representation for that. Not
// modelled, deliberately.
//
// WHAT THIS TEST ACTUALLY GUARDS, stated because it is not what it looks like.
// It does NOT pin the `target > i` exclusion in jumpedOver. Two mutations were
// run against it — treating backward jumps as forward, and marking the whole
// backward span optional — and BOTH survived. The reason is upstream of the
// goto logic entirely: `top: q += …` is a LabeledStmt, which foldStmts' default
// case hands to untrackAssigned, so q is untracked before any of this matters.
//
// So the backward exclusion is defensive and unpinnable by output, and saying so
// is worth more than a test that appears to cover it. What this DOES pin is the
// property that matters: this shape emits no folded world. If a later change
// made labeled statements fold rather than untrack, that world would appear and
// this test would catch it.
func TestBackwardGotoEmitsNoFabricatedWorld(t *testing.T) {
	src := "package db\n\nfunc q(b bool) string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"top:\n\tq += \" X\"\n" +
		"\tif b {\n\t\tgoto top\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n" +
		"\treturn q\n}\n"
	if worlds := foldOnly(foldTexts(t, src), "SELECT id FROM t"); len(worlds) != 0 {
		t.Fatalf("a backward goto must not produce a folded world; got %q", worlds)
	}
}
