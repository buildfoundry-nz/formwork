// fold_write_forms_test.go — the sweep, after #72, #73, #74, #310, #337, #314.
//
// Six issues, one defect: a write the fold does not see is dropped while the
// variable stays TRACKED, so the fold emits a value assembled from only the
// writes it happened to see — a query no execution path produces. Each was found
// and fixed at ONE syntax form, and the next form was still open when the next
// review looked. #314 is the third issue in that sequence and the range clause
// was never named by any of them.
//
// So this file is not another shape. It is the enumeration: every statement form
// this pass can meet that writes, or looks like it writes, a tracked variable,
// each with the verdict it must produce AND THE REASON, in one table. A form
// nobody has written a fix for is still in it — if a later change makes `switch`
// or `select` fold, this table says so on the next run rather than on the next
// adversarial review.
//
// TWO VERDICTS ARE BOTH CORRECT, and telling them apart is the whole content:
//
//   - NO WORLD. The write is real and this walk cannot model it, so the variable
//     is untracked and nothing is emitted. A miss — the fold stays silent about
//     a query it cannot assemble.
//   - THE WORLD. The write does not touch the tracked variable, or does not
//     happen before its value is used, so what the fold emits is what the program
//     produces. Untracking here would delete a true positive, which is the cost
//     that killed this fold's first design (#72: 10 findings removed, 8 of them
//     real).
//
// WHAT THIS TABLE DOES NOT SWEEP: which names must never be tracked at all,
// because a write to them is invisible everywhere — a taken address, a closure
// reached other than by its name. That is unseenwrite.go's question and it is
// swept by fold_escape_test.go and fold_closure_call_test.go. The range clause
// has its own file (fold_range_clause_test.go) because its verdict depends on
// the range SOURCE rather than on the statement form; one case of each verdict
// is repeated here so the enumeration is readable as one list.
package sqlextract_test

import (
	"slices"
	"testing"
)

// writeSeed is a locking SELECT once " FOR UPDATE" is appended — the shape
// sql/locking-select-order fires on, so a fabricated world here is a false
// deadlock finding and a dropped one is a silenced hazard.
const writeSeed = "SELECT id FROM t WHERE s = 'x'"

// formSrc wraps one statement form in the standard shape: seed, the form, an
// append, a return.
func formSrc(params, form string) string {
	return "package db\n\nfunc f(" + params + ") string {\n" +
		"\tq := \"" + writeSeed + "\"\n" + form +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
}

func TestEveryWriteFormHasAVerdict(t *testing.T) {
	folded := []string{writeSeed + " FOR UPDATE"}
	cases := []struct {
		name   string
		src    string
		want   []string
		reason string
	}{
		// ---- The write reaches q through a construct this walk does not model.
		// Untracked, so no world at all.
		{"select-case-receive", formSrc("ch chan string",
			"\tselect {\n\tcase q = <-ch:\n\t}\n"), nil,
			"the receive writes q on one arm of a construct this walk does not model"},
		{"type-switch-arm", formSrc("v any",
			"\tswitch t := v.(type) {\n\tcase string:\n\t\tq = t\n\t}\n"), nil,
			"an arm writes q, and which arm runs is not modelled"},
		{"switch-arm", formSrc("n int",
			"\tswitch n {\n\tcase 1:\n\t\tq = \"x\"\n\t}\n"), nil,
			"same as the type switch: the arm is a path this walk does not take"},
		{"switch-init", formSrc("n int",
			"\tswitch q = \"x\"; n {\n\tcase 1:\n\t}\n"), nil,
			"a switch's Init runs unconditionally, and is still not a statement " +
				"foldStmts folds"},
		{"for-init", formSrc("",
			"\tfor q = \"x\"; false; {\n\t}\n"), nil,
			"the loop's Init writes q before any iteration"},
		{"for-post", formSrc("n int",
			"\tfor i := 0; i < n; q = \"x\" {\n\t\tbreak\n\t}\n"), nil,
			"the Post writes q once per iteration, and the count is unknown"},
		{"for-body", formSrc("n int",
			"\tfor i := 0; i < n; i++ {\n\t\tq = \"x\"\n\t}\n"), nil,
			"the body writes q an unknown number of times, including none"},
		{"multi-assignment", formSrc("",
			"\tvar z string\n\tq, z = \"x\", \"y\"\n\t_ = z\n"), nil,
			"a multi-target assignment is not a shape foldAssign tracks, and it " +
				"writes q"},
		{"bare-block", formSrc("",
			"\t{\n\t\tq = \"x\"\n\t}\n"), nil,
			"a block is not folded into its parent, so the write inside it is unseen"},
		{"if-else-both-arms", formSrc("b bool",
			"\tif b {\n\t\tq = \"x\"\n\t} else {\n\t\tq = \"y\"\n\t}\n"), nil,
			"a mandatory choice is untracked wholesale rather than forked (spec §4.2)"},
		{"labelled-statement", formSrc("",
			"\tgoto L\nL:\n\tq = \"x\"\n"), nil,
			"a LabeledStmt reaches foldStmts' default arm, whatever it wraps"},
		{"backward-goto", formSrc("b bool",
			"top:\n\tq += \" ORDER BY id\"\n\tif b {\n\t\tgoto top\n\t}\n"), nil,
			"the appends between label and branch repeat an unknown number of " +
				"times; the LabeledStmt untracks before the goto logic matters"},
		{"range-clause-over-an-array", formSrc("",
			"\tvar arr [2]string\n\tfor _, q = range arr {\n\t}\n"), nil,
			"the loop certainly runs, so the seed is certainly gone (#314)"},

		// ---- The write does not reach q, or does not reach it before its value
		// is used. The emitted world is what the program produces.
		{"increment", formSrc("n int",
			"\tn++\n\t_ = n\n"), folded,
			"an IncDecStmt cannot target a string, so it never writes a tracked " +
				"variable; untracking on one would delete a candidate for nothing"},
		{"map-value-write", formSrc("m map[string]string",
			"\tm[\"k\"] = q\n\tm[\"k\"] += \" ORDER BY id\"\n"), folded,
			"a map value is its own storage: appending to it leaves q as it was"},
		{"struct-field-write", formSrc("",
			"\tvar s struct{ f string }\n\ts.f = q\n\ts.f += \" ORDER BY id\"\n"), folded,
			"a field is its own storage too — the copy into it is not an alias"},
		{"range-define", formSrc("m map[string]string",
			"\tfor _, q := range m {\n\t\t_ = q\n\t}\n"), folded,
			"`:=` binds a new q in the loop's own scope; the outer one is never written"},
		{"deferred-closure", formSrc("",
			"\tdefer func() { q += \" ORDER BY id\" }()\n"), folded,
			"the deferred call runs after `return q` has read q, so the returned " +
				"query is the one without its append"},
		{"nested-immediate-invocations", formSrc("",
			"\tfunc() {\n\t\tfunc() {\n\t\t\tq += \" ORDER BY id\"\n\t\t}()\n\t}()\n"),
			[]string{writeSeed + " ORDER BY id FOR UPDATE"},
			"both literals are invoked in statement position where they sit, so " +
				"their appends are folded in at that point rather than dropped"},

		// ---- Composition that never seeds a tracked variable at all.
		{"strings-builder", "package db\n\nimport \"strings\"\n\nfunc f() string {\n" +
			"\tvar sb strings.Builder\n\tsb.WriteString(\"" + writeSeed + "\")\n" +
			"\tsb.WriteString(\" FOR UPDATE\")\n\treturn sb.String()\n}\n", nil,
			"a builder composes through method calls on a value the fold never " +
				"tracks — it holds no string literal seed — so there is no world to " +
				"emit and none to fabricate"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := foldOnly(foldTexts(t, c.src), writeSeed)
			slices.Sort(got)
			want := slices.Clone(c.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("fold worlds = %q, want %q — %s", got, want, c.reason)
			}
		})
	}
}
