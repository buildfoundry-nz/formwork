package sqlextract_test

import "testing"

// A parenthesised assignment target (#72). `(q) += …` is valid Go, survives
// gofmt, and is an *ast.ParenExpr — so matching a bare *ast.Ident misses it.
// foldAssign then RETURNS WITHOUT UNTRACKING, leaving the variable tracked while
// the append vanishes, and the fold emits a value no path produces. No closure
// is involved: this reaches plain block level. foldGuard and guardPath two
// functions away already read through ast.Unparen. Spec §4.1, §4.2.

const lhsSeed = "SELECT id FROM t WHERE s = 'x'"

func lhsFolds(t *testing.T, body string) []string {
	t.Helper()
	src := "package db\n\nvar m map[string]int\n\nfunc load(b bool) string {\n" + body + "\treturn q\n}\n"
	return foldOnly(foldTexts(t, src), lhsSeed)
}

func TestFromGoReassembledParenthesisedTargetIsSeen(t *testing.T) {
	// Each body appends via a parenthesised target and then locks. The emitted
	// world must never be the one that silently drops the parenthesised append.
	bad := lhsSeed + " FOR UPDATE"
	for _, tc := range []struct{ name, body string }{
		{"plain +=", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\t(q) += \" ORDER BY id\"\n\tq += \" FOR UPDATE\"\n"},
		{"double parens", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\t((q)) += \" ORDER BY id\"\n\tq += \" FOR UPDATE\"\n"},
		{"inside a for body", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tfor b {\n\t\t(q) += \" ORDER BY id\"\n\t}\n\tq += \" FOR UPDATE\"\n"},
		{"multi-assign", "\tq := `SELECT id FROM t WHERE s = 'x'`\n" +
			"\tvar z int\n\t(q), z = \" ORDER BY id\", 3\n\t_ = z\n\tq += \" FOR UPDATE\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if folds := lhsFolds(t, tc.body); hasFoldText(folds, bad) {
				t.Fatalf("a parenthesised target must not be dropped while the variable stays tracked: %q", folds)
			}
		})
	}
}

func TestFromGoReassembledUnparenthesisedTargetStillFolds(t *testing.T) {
	// PRECISION guard, green throughout: reading through ast.Unparen must not
	// change the ordinary path. A plain `q += …` still folds.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n\tq += \" ORDER BY id FOR UPDATE\"\n"
	want := lhsSeed + " ORDER BY id FOR UPDATE"
	if folds := lhsFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("an ordinary append must still fold: want %q in %q", want, folds)
	}
}

func TestFromGoReassembledSelectorTargetStillIgnored(t *testing.T) {
	// PRECISION guard, green throughout: `x.q += …` is a selector, not a tracked
	// name, and unparenthesising must not turn it into one.
	body := "\tq := `SELECT id FROM t WHERE s = 'x'`\n\tvar x struct{ q string }\n" +
		"\tx.q += \" NONSENSE\"\n\tq += \" ORDER BY id FOR UPDATE\"\n"
	want := lhsSeed + " ORDER BY id FOR UPDATE"
	if folds := lhsFolds(t, body); !hasFoldText(folds, want) {
		t.Fatalf("a selector target must not disturb tracking: want %q in %q", want, folds)
	}
}
