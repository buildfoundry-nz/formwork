package sqlextract_test

import "testing"

// Guard-shape and pair-eligibility tests for the assignment-flow fold — split
// out of fold_test.go, which the tables below pushed toward the 750-line vendor
// cap. These answer "which guards name a pair, and what survives whatever the
// guards do"; fold_test.go answers "which worlds does a named pair produce".
// Shared helpers (foldTexts, hasFoldText, foldOnly) live in fold_test.go.

// EVERY WORLD-SHAPE ASSERTION ABOVE IS ABOUT WHAT THE FOLD ADDS. These are
// about what it must never take away.
//
// `full` and `base` are emitted for every appended variable, whatever its
// guards look like — `foldWorlds` seeds the list with both before it looks at a
// single guard. No input can make a row here fail today, and that is the point:
// each row is a shape in which one of the four abandoned suppression attempts
// wrongly PROVED a complementary pair, deleted a reachable world, and silenced a
// real unordered-locking SELECT (§10). They are a standing guard against
// suppression returning, not a check on live branching logic — so they live in
// one table instead of fifteen near-identical funcs that read like coverage of
// a write analysis this pass no longer has.
//
// The nine shapes are the ones enumerated on #42. Each is four lines of Go, and
// any future suppression proposal must be run against all of them BEFORE it is
// implemented.
func TestFromGoReassembledBoundedPairSurvivesEveryGuardShape(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // the world that must survive, whatever the guards do
	}{
		{
			// GENUINELY INDEPENDENT guards: a=false,b=false really is
			// reachable, so base is a real unordered lock, not a #42 artifact.
			name: "independent guards",
			src: "package db\n\nfunc q(a, b bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tq += \" AND a1 = 1\"\n\t}\n" +
				"\tif b {\n\t\tq += \" AND b1 = 2\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			// The case that ended the effort: `run(s)` observes the query
			// BETWEEN the branches, so base is what the callee gets on a real
			// path. There is no write anywhere for an analysis to find —
			// complementarity constrains the FINAL value only.
			name: "query observed between the branches",
			src: "package db\n\nfunc run(s string) {}\n\nfunc q(a bool) string {\n" +
				"\ts := \"SELECT id FROM t\"\n\ts += \" FOR UPDATE\"\n" +
				"\tif a {\n\t\ts += \" ORDER BY id\"\n\t}\n" +
				"\trun(s)\n" +
				"\tif !a {\n\t\ts += \" ORDER BY name\"\n\t}\n" +
				"\treturn s\n}\n",
			want: "SELECT id FROM t FOR UPDATE",
		},
		{
			name: "guard reassigned between the branches",
			src: "package db\n\nfunc q(a bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\ta = true\n" +
				"\tif !a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			name: "guard reassigned by a range clause",
			src: "package db\n\nfunc q(a bool, xs []bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tfor _, a = range xs {\n\t}\n" +
				"\tif !a {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			// A free variable of an enclosing scope: a sibling closure this
			// block never walks can write it between the two reads.
			name: "guard captured from an enclosing scope",
			src: "package db\n\nfunc outer(a bool) (func(), func() string) {\n" +
				"\tset := func() {\n\t\ta = true\n\t}\n" +
				"\tbuild := func() string {\n" +
				"\t\tq := \"SELECT id FROM t\"\n" +
				"\t\tif a {\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n" +
				"\t\tif !a {\n\t\t\tq += \" ORDER BY name\"\n\t\t}\n" +
				"\t\tq += \" FOR UPDATE\"\n\t\treturn q\n\t}\n" +
				"\treturn set, build\n}\n",
			want: "SELECT id FROM t FOR UPDATE",
		},
		{
			// The address escapes inside an if-CONDITION — the one expression
			// position a write scan is most likely to skip.
			name: "guard address taken in an if-condition",
			src: "package db\n\nfunc grab(p *bool) bool { return *p }\n\nfunc q(a bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tif grab(&a) {\n\t\t_ = a\n\t}\n" +
				"\tif !a {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			// Declared ABOVE the query: where a closure literal sits says
			// nothing about when it runs.
			name: "closure declared before the query flips the guard",
			src: "package db\n\nfunc q(a bool) string {\n" +
				"\tflip := func() {\n\t\ta = !a\n\t}\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tflip()\n" +
				"\tif !a {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			// The commonest builder shape: a helper handed the options struct.
			// Parse-only cannot tell a pointer parameter from a value one.
			name: "options struct handed to a call",
			src: "package db\n\ntype opt struct{ Ordered bool }\n\nfunc normalize(o *opt) {}\n\n" +
				"func q(o *opt) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tnormalize(o)\n" +
				"\tif !o.Ordered {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			// No argument at all, only a receiver.
			name: "pointer-receiver method on the guard's struct",
			src: "package db\n\ntype opt struct{ Ordered bool }\n\nfunc (o *opt) Reset() {}\n\n" +
				"func q(o *opt) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\to.Reset()\n" +
				"\tif !o.Ordered {\n\t\tq += \" ORDER BY name, id\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' FOR UPDATE",
		},
		{
			// A method VALUE: `tg()` names no receiver at the call site.
			name: "method value that can flip the guard",
			src: "package db\n\ntype opt struct{ Ordered bool }\n\nfunc (o *opt) Toggle() {}\n\n" +
				"func q(o *opt) string {\n" +
				"\ts := \"SELECT id FROM t\"\n\ttg := o.Toggle\n\ts += \" FOR UPDATE\"\n" +
				"\tif o.Ordered {\n\t\ts += \" ORDER BY id\"\n\t}\n" +
				"\ttg()\n" +
				"\tif !o.Ordered {\n\t\ts += \" ORDER BY name\"\n\t}\n" +
				"\treturn s\n}\n",
			want: "SELECT id FROM t FOR UPDATE",
		},
		{
			// `p := o` is a second name for the same storage.
			name: "pointer copy of the guard's struct",
			src: "package db\n\ntype opt struct{ Ordered bool }\n\nfunc q(o *opt) string {\n" +
				"\tq := \"SELECT id FROM t\"\n" +
				"\tif o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tp := o\n\tp.Ordered = true\n" +
				"\tif !o.Ordered {\n\t\tq += \" LIMIT 10\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t FOR UPDATE",
		},
		{
			// Flipped INSIDE its own branch, above the append it guards: with
			// a=true the branch runs, sets a false, and the `!a` branch runs
			// too — so the both-branches world (`full`) is the executed query.
			name: "guard flipped inside its own branch",
			src: "package db\n\nfunc q(a bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE id = $1\"\n" +
				"\tif a {\n\t\ta = false\n\t\tq += \" OR s = 'x'\"\n\t}\n" +
				"\tif !a {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
				"\treturn q\n}\n",
			want: "SELECT id FROM t WHERE id = $1 OR s = 'x' FOR UPDATE",
		},
		{
			name: "guard flipped by a closure between the branches",
			src: "package db\n\nfunc q(a bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tflip := func() {\n\t\ta = !a\n\t}\n\tflip()\n" +
				"\tif !a {\n\t\tq += \" ORDER BY name\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
			want: "SELECT id FROM t WHERE s = 'x' ORDER BY id ORDER BY name FOR UPDATE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if texts := foldTexts(t, tt.src); !hasFoldText(texts, tt.want) {
				t.Errorf("the bounded pair must survive every guard shape: %q not in %q", tt.want, texts)
			}
		})
	}
}

// AN OPAQUE CONDITION IS NEVER FORCED BY THE GUARD ENCLOSING IT — the other
// half of the `opaque` flag, and the one with a hazard riding on it.
//
// The nested ` ORDER BY id` here fires on `a && call()`. Fixing a=true settles
// its recorded guard list ([a]) completely, so without the opaque check segIn
// would report it segHolds and render it ON in every a=true world. It is not
// forced: `call()` can be false. The world that assignment describes — a=true,
// call() false — is `… FOR UPDATE` with no ORDER BY, a genuine unordered lock,
// and it is reachable.
//
// `base` does NOT cover it: the lock is optional here, so base does not lock at
// all. Drop `!seg.opaque` from segIn and this world stops being emitted while
// every other test in this package stays green — which is how it was found.
func TestFromGoReassembledOpaqueNestedConditionIsNotForcedByItsGuard(t *testing.T) {
	src := "package db\n\nfunc call() bool { return true }\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
		"\tif a {\n\t\tif call() {\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n\t}\n" +
		"\tif a {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" LIMIT 1\"\n\t}\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	want := "SELECT id FROM t WHERE s = 'x' FOR UPDATE"
	if !hasFoldText(texts, want) {
		t.Fatalf("a=true with the opaque inner condition false is an unordered lock: %q not in %q", want, texts)
	}
}

// WHERE THE COMPLEMENTARY-PAIR ENUMERATION STOPS, asserted as an EXACT world
// count rather than a presence check.
//
// A pair is collected only from an append whose SOLE guard reads a stored value
// directly (foldGuard, complementaryCandidates). Every shape below is ruled
// ineligible, so the variable stays on the bounded pair: exactly two fold
// worlds, `full` and `base`.
//
// This is the one direction in this file that can go SILENT. Ruling a shape
// ineligible WITHHOLDS the branch worlds, and the branch worlds are what catch a
// lock and an order split across one flag's polarities (§9 miss 1) — so a
// widened or broken eligibility test loses that hazard and no
// `base`-is-emitted assertion would notice. Pinning the exact count is what
// makes the boundary visible; move it deliberately, not by accident.
func TestFromGoReassembledPairEligibilityBoundary(t *testing.T) {
	const seed = "SELECT id FROM t WHERE s = 'x'"
	tests := []struct {
		name string
		src  string
	}{
		{
			// A CALL is not a proposition: two evaluations of `opts.Order()`
			// can return different values (mutable state, a clock).
			name: "call guard",
			src: "package db\n\ntype o struct{}\n\nfunc (o) Order() bool { return true }\n\n" +
				"func q(opts o) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif opts.Order() {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tif !opts.Order() {\n\t\tq += \" ORDER BY name\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
		},
		{
			// An `if`-Init may BIND the name its condition tests, so these two
			// `ok`s are separate lookups: first false and second true is a real
			// path on which neither branch appends.
			name: "if-Init bound guard",
			src: "package db\n\nfunc q(m map[string]string) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif _, ok := m[\"a\"]; ok {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tif _, ok := m[\"b\"]; !ok {\n\t\tq += \" ORDER BY name\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
		},
		{
			// A compound condition is not one proposition; `!(a && c)` is not
			// the negation this pass can name.
			name: "compound condition",
			src: "package db\n\nfunc q(a, c bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a && c {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tif !(a && c) {\n\t\tq += \" ORDER BY name\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
		},
		{
			// A NESTED append fires on a conjunction (`a && b`), so it says
			// nothing about an `if !b` elsewhere. Pairing on the innermost
			// guard alone would invent the pair.
			name: "nested append is not paired on its innermost guard",
			src: "package db\n\nfunc q(a, b bool) string {\n" +
				"\tq := \"SELECT id FROM t WHERE s = 'x'\"\n" +
				"\tif a {\n\t\tif b {\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n\t}\n" +
				"\tif !b {\n\t\tq += \" ORDER BY name\"\n\t}\n" +
				"\tq += \" FOR UPDATE\"\n\treturn q\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			folds := foldOnly(foldTexts(t, tt.src), seed)
			if len(folds) != 2 {
				t.Errorf("an ineligible guard leaves the variable on the bounded pair, want exactly 2 worlds, got %d: %q", len(folds), folds)
			}
		})
	}
}
