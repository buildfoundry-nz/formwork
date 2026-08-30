package sqlextract_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// Tests for the assignment-flow fold (#36, #42) — fold.go's half of this
// package. Split out of sqlextract_test.go, which the fold's world-model tests
// pushed past the repo's 750-line vendor cap (.formwork/rules/file-size.yaml);
// the boundary follows the source's own, so fold.go's tests live beside the
// reassembly tests rather than inside them.
//
// The shared helpers (foldTexts, hasFoldText, foldOnly) live here because every
// caller does.

func TestFromGoReassembledFoldsUnconditionalAppend(t *testing.T) {
	// `q := <literal>` then `q += <literal>` across two statements folds to ONE
	// joined candidate — the shape the expression walk alone cannot see (#36).
	src := "package db\n\nfunc q() string {\n\tq := \"SELECT * FROM t WHERE x=1\"\n\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "SELECT * FROM t WHERE x=1 FOR UPDATE"
	found := false
	for _, c := range got {
		if c.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("want joined candidate %q, got %+v", want, got)
	}
}

func TestFromGoReassembledFoldsInsideFuncLit(t *testing.T) {
	// A func-literal body is its own scope root; folding must run there too.
	src := "package db\n\nvar f = func() string {\n\tq := \"SELECT * FROM t\"\n\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	found := false
	for _, c := range got {
		if c.Text == "SELECT * FROM t FOR UPDATE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fold must run inside a func literal body: %+v", got)
	}
}

func TestFromGoReassembledSeededButUnappendedEmitsNoJoin(t *testing.T) {
	// A `:=` with no following `+=` yields no folded candidate — the expression
	// walk already emits the seed literal (additivity: no duplicate).
	src := "package db\n\nfunc q() string {\n\tq := \"SELECT * FROM t\"\n\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	n := 0
	for _, c := range got {
		if c.Text == "SELECT * FROM t" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("seed must appear exactly once (walk only, no fold dup): got %d in %+v", n, got)
	}
}

func TestFromGoReassembledForLoopAppendNotFolded(t *testing.T) {
	// Soundness guard: a variable appended inside a for loop is untracked, so
	// folding never emits a stale pre-loop value.
	src := "package db\n\nfunc q(xs []string) string {\n\tq := \"SELECT * FROM t FOR UPDATE\"\n\tfor range xs {\n\t\tq += \" x\"\n\t}\n\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, c := range got {
		if strings.Contains(c.Text, "FOR UPDATE x") {
			t.Fatalf("loop-built value must not be folded: %+v", c)
		}
	}
}

func TestFromGoReassembledBareBlockShadowNotFabricated(t *testing.T) {
	// A `q := …` inside a bare { } block DECLARES a new variable scoped to that
	// block; the outer q is untouched. Folding must not overwrite the outer
	// tracking with the shadow and fabricate a value no real code path holds.
	src := "package db\n\nfunc q() string {\n\tq := \"SELECT 1\"\n\t{\n\t\tq := \"SELECT 2\"\n\t\t_ = q\n\t}\n\tq += \" X\"\n\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, c := range got {
		if c.Text == "SELECT 2 X" {
			t.Fatalf("bare-block shadow must not fabricate a candidate: %+v", got)
		}
	}
}

func TestFromGoReassembledFoldsOptionalIfAppend(t *testing.T) {
	// `q := …` then `q += …` inside an if-without-else: the joined "full" value
	// (base + the optional append) is emitted as a candidate at the extractor
	// unit level. (#36, plan §7)
	src := "package db\n\nfunc q(lock bool) string {\n\tq := \"SELECT * FROM t\"\n\tif lock {\n\t\tq += \" FOR UPDATE\"\n\t}\n\treturn q\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	found := false
	for _, c := range got {
		if c.Text == "SELECT * FROM t FOR UPDATE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("optional if-branch append must emit the joined full candidate: %+v", got)
	}
}

// EMISSION IS A SUPERSET OF THE PRE-#42 MODEL, ALWAYS. `full` and `base` are
// emitted for every appended variable, whatever its guards look like, and the
// complementary branch worlds are added on top. Nothing the bounded pair
// reported before can go silent, because nothing is ever taken away.
//
// The cost is #42 itself: `base` here is the "neither branch ran" world, which
// a complementary pair proves unreachable, so the false positive that opened
// #42 remains. Suppressing it needs proof that two reads see one value, and
// four review rounds established that a parse-only pass cannot carry that proof
// — a wrongly proven pair deletes a reachable world and silences the gate,
// which is the worse failure. Disclosed in spec §9 and in locking.go.
func TestFromGoReassembledAlwaysEmitsTheBoundedPair(t *testing.T) {
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	texts := foldTexts(t, src)
	for _, want := range []string{
		"SELECT id FROM t WHERE status = 'x' FOR UPDATE",                         // base
		"SELECT id FROM t WHERE status = 'x' ORDER BY id ORDER BY id FOR UPDATE", // full
		"SELECT id FROM t WHERE status = 'x' ORDER BY id FOR UPDATE",             // each branch
	} {
		if !hasFoldText(texts, want) {
			t.Errorf("the bounded pair and the branch worlds must all be emitted: %q not in %q", want, texts)
		}
	}
}

// foldTexts collects candidate texts for the emission-set assertions below.
func foldTexts(t *testing.T, src string) []string {
	t.Helper()
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	out := make([]string, 0, len(got))
	for _, c := range got {
		out = append(out, c.Text)
	}
	return out
}

func hasFoldText(texts []string, want string) bool {
	return slices.Contains(texts, want)
}

// foldOnly keeps the fold worlds — the texts built on seed — and drops the bare
// literals the expression walk emits alongside them.
func foldOnly(texts []string, seed string) []string {
	out := []string{}
	for _, got := range texts {
		if got != seed && strings.HasPrefix(got, seed) {
			out = append(out, got)
		}
	}
	return out
}

// Complementary guards make "neither branch ran" unreachable, but they do NOT
// make either ONE-BRANCH world unreachable — and when the branches append
// DIFFERENT text, a one-branch world is the only model of a real path. Here the
// `!a` path is `… LIMIT 10 FOR UPDATE`: a locking SELECT with no ORDER BY, a
// genuine hazard. Suppressing `base` and emitting only `full` (both appends)
// loses it, so each branch's world must be emitted.
func TestFromGoReassembledComplementaryPairEmitsEachBranchWorld(t *testing.T) {
	src := "package db\n\nfunc q(a bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" LIMIT 10\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	texts := foldTexts(t, src)
	for _, want := range []string{
		"SELECT id FROM t WHERE status = 'x' ORDER BY id FOR UPDATE",
		"SELECT id FROM t WHERE status = 'x' LIMIT 10 FOR UPDATE",
	} {
		if !hasFoldText(texts, want) {
			t.Errorf("complementary branch world must be emitted: %q not in %q", want, texts)
		}
	}
}

// Suppression must be scoped to the appends the complementary pair actually
// covers. An INDEPENDENT optional append alongside the pair leaves its own
// "false" world reachable: with wantOrder=false the query is a locking SELECT
// with no ORDER BY. Dropping `base` for the whole variable leaves only worlds
// that carry the ORDER BY, so the hazard goes unmodelled.
func TestFromGoReassembledComplementaryPairKeepsIndependentOptionalOffWorld(t *testing.T) {
	src := "package db\n\nfunc q(a, wantOrder bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" AND p = 1\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" AND r = 2\"\n\t}\n" +
		"\tif wantOrder {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	texts := foldTexts(t, src)
	for _, want := range []string{
		"SELECT id FROM t WHERE status = 'x' AND p = 1 FOR UPDATE",
		"SELECT id FROM t WHERE status = 'x' AND r = 2 FOR UPDATE",
	} {
		if !hasFoldText(texts, want) {
			t.Errorf("pair world with the independent optional OFF must be emitted: %q not in %q", want, texts)
		}
	}
}

// The spec's §9 miss 1 sub-case: an optional ORDER BY and an optional lock split
// across ONE flag's opposite polarities. Both all-or-nothing extremes are
// infeasible (`full` takes both, `base` neither), so the real x=true path —
// locking, unordered — used to be modelled by no candidate at all. Enumerating
// one branch per complementary name reaches it.
func TestFromGoReassembledOppositePolarityOrderAndLockEmitsLockOnlyWorld(t *testing.T) {
	src := "package db\n\nfunc q(x bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif !x {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif x {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	want := "SELECT id FROM t WHERE status = 'x' FOR UPDATE"
	if !hasFoldText(texts, want) {
		t.Fatalf("the x=true world (lock on, order off) must be emitted: %q not in %q", want, texts)
	}
}

// PAST maxEnumeratedPairs THE ENUMERATION TRUNCATES — it does not collapse.
// Four pairs here; the first three (sorted, so deterministically a, b and c)
// get their branch worlds and d does not, giving 2 + 4×3 = 14 fold worlds.
//
// The truncation is a COST bound, not a claim about reachability: every text is
// a pg_query parse on wazero and those dominate the rule's runtime. What matters
// is the direction it degrades. The previous rule collapsed to full/base the
// moment a second pair appeared, so more flags bought less analysis; truncating
// keeps everything the one-pair rule emitted and more, and d's appends are still
// covered by the all-or-nothing bound. Disclosed in locking.go and spec §9.
func TestFromGoReassembledPairEnumerationTruncatesPastTheCap(t *testing.T) {
	src := "package db\n\nfunc q(a, b, c, d bool) string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\tif a {\n\t\tq += \" AND a1\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" AND a2\"\n\t}\n" +
		"\tif b {\n\t\tq += \" AND b1\"\n\t}\n" +
		"\tif !b {\n\t\tq += \" AND b2\"\n\t}\n" +
		"\tif c {\n\t\tq += \" AND c1\"\n\t}\n" +
		"\tif !c {\n\t\tq += \" AND c2\"\n\t}\n" +
		"\tif d {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !d {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	texts := foldTexts(t, src)
	for _, want := range []string{
		"SELECT id FROM t FOR UPDATE", // base — always
		"SELECT id FROM t AND a1 AND a2 AND b1 AND b2 AND c1 AND c2 ORDER BY id ORDER BY id FOR UPDATE", // full — always
		"SELECT id FROM t AND a2 FOR UPDATE", // the a=false branch, enumerated
		"SELECT id FROM t AND c1 FOR UPDATE", // the c=true branch, still under the cap
	} {
		if !hasFoldText(texts, want) {
			t.Errorf("truncation keeps the bounded pair and the pairs under the cap: %q not in %q", want, texts)
		}
	}
	// 14, not 18: d is past the cap. Exact, so widening the cap without
	// revisiting the parse-cost argument shows up here.
	if n := len(foldOnly(texts, "SELECT id FROM t")); n != 14 {
		t.Errorf("want 2 + 4x3 = 14 fold worlds at the cap, got %d: %q", n, texts)
	}
}

// A nested optional append is reachable only when its ENCLOSING branch is too.
// In the a=false world the nested ` FOR UPDATE` cannot fire, so no world may
// pair it with the `!a` branch's text.
func TestFromGoReassembledNestedOptionalKeepsItsEnclosingGuard(t *testing.T) {
	src := "package db\n\nfunc q(a, b bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t\tif b {\n\t\t\tq += \" FOR UPDATE\"\n\t\t}\n\t}\n" +
		"\tif !a {\n\t\tq += \" LIMIT 1\"\n\t}\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	if impossible := "SELECT id FROM t WHERE status = 'x' FOR UPDATE LIMIT 1"; hasFoldText(texts, impossible) {
		t.Errorf("the nested lock needs a; it cannot co-occur with the !a branch: %q", texts)
	}
	if want := "SELECT id FROM t WHERE status = 'x' ORDER BY id FOR UPDATE"; !hasFoldText(texts, want) {
		t.Errorf("the a&&b world is reachable and must be emitted: %q not in %q", want, texts)
	}
}

// A PAIR ONE NESTING LEVEL DOWN IS STILL A PAIR. Both appends sit under the
// same `if useTx`, so their guard conjunctions are `useTx && a` and
// `useTx && !a` — identical prefix, opposite last polarity. Exactly as
// complementary as the top-level pair, and for the same reason.
//
// Requiring a SOLE guard missed this, and the miss was silent: `full` is the
// only fold world (base equals the seed and is dropped), so the real
// useTx=true,a=false path — `… FOR UPDATE` with no ORDER BY — was modelled by
// nothing and the gate passed it clean. A transaction guard wrapping the
// builder is the commonest shape in this codebase's target, which is what makes
// it worth pairing rather than disclosing.
//
// The sole-guard rule was right about the case it was written for — an append
// under `if a { if b { … } }` says nothing about an `if !b` elsewhere, because
// the PREFIXES differ. Keying on (prefix, last guard) keeps that exclusion and
// admits this one; TestFromGoReassembledPairEligibilityBoundary pins the
// difference.
func TestFromGoReassembledPairUnderSharedGuardEmitsItsBranchWorlds(t *testing.T) {
	src := "package db\n\nfunc build(a, useTx bool) string {\n" +
		"\tq := \"SELECT * FROM t WHERE s='x'\"\n" +
		"\tif useTx {\n" +
		"\t\tif a {\n\t\t\tq += \" ORDER BY id\"\n\t\t}\n" +
		"\t\tif !a {\n\t\t\tq += \" FOR UPDATE\"\n\t\t}\n" +
		"\t}\n\treturn q\n}\n"
	texts := foldTexts(t, src)
	want := "SELECT * FROM t WHERE s='x' FOR UPDATE"
	if !hasFoldText(texts, want) {
		t.Fatalf("the useTx&&!a world is a locking SELECT with no ORDER BY: %q not in %q", want, texts)
	}
}

// MORE PAIRS MUST NOT BUY FEWER WORLDS. A variable with two complementary
// flags has strictly more hazard surface than one with a single flag; capping
// the enumeration at one pair gave it strictly less analysis — the second pair
// dropped the variable to `full` and `base` alone.
//
// Here neither extreme is a hazard: `full` carries the ORDER BY, `base` takes
// no lock. The hazard lives entirely in the b=false worlds — `… FOR UPDATE`
// with no ORDER BY, reachable on every value of a — so the one-pair cap made it
// invisible.
//
// Each pair is enumerated SEPARATELY, one truth value fixed per world. That is
// what keeps the cross-product out: no world fixes a and b together, so no
// mixed-flag world is invented (see
// TestFromGoReassembledSeveralPairsInventNoMixedWorld, which still holds).
func TestFromGoReassembledEachPairEmitsItsOwnBranchWorlds(t *testing.T) {
	src := "package db\n\nfunc build(a, b bool) string {\n" +
		"\tq := \"SELECT id FROM t WHERE s='x'\"\n" +
		"\tif a {\n\t\tq += \" AND p = 1\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" AND r = 2\"\n\t}\n" +
		"\tif b {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif !b {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	want := "SELECT id FROM t WHERE s='x' FOR UPDATE"
	if !hasFoldText(texts, want) {
		t.Fatalf("the b=false world is a locking SELECT with no ORDER BY: %q not in %q", want, texts)
	}
}

// WHY PAIRS ARE ENUMERATED SEPARATELY rather than multiplied, and the reason
// this is not a correlation analysis.
//
// `b := a` makes a=false,b=true unreachable, so a cross-product over both pairs
// invents `… LIMIT 1 FOR UPDATE` — an unordered lock on code whose every real
// path either orders and locks or does neither. That is #42's false positive
// again, one flag copy away, and no rewrite of the SQL clears it.
//
// NOTHING HERE INSPECTS `b := a`. The fold does not detect correlation and has
// no facts about it; the mixed world stays unemitted because foldWorlds fixes
// exactly ONE truth value per world, so no world ever asserts a and b together —
// whatever the flags are, and whichever side the copy sits on. Read these as
// "pairs are not multiplied out", not as "correlated flags are understood":
// enumerating the cross-product would need the independence proof §10 rules out.
//
// Correlation is not free, and this test does not claim it is. Fixing ONE flag
// can still describe an unreachable world when another flag is a copy — that
// costs a false positive, pinned at the gate by
// TestLockingCorrelatedGuardPairsFireAsDisclosed. What is ruled out here is the
// stronger failure of inventing a world that mixes two flags' branches.
func TestFromGoReassembledSeveralPairsInventNoMixedWorld(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "flag copied before the first pair",
			src: "package db\n\nfunc q(a bool) string {\n" +
				"\tb := a\n" +
				"\tq := \"SELECT id FROM t\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tif !a {\n\t\tq += \" LIMIT 1\"\n\t}\n" +
				"\tif b {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
				"\tif !b {\n\t\tq += \" LIMIT 2\"\n\t}\n" +
				"\treturn q\n}\n",
		},
		{
			name: "flag copied after the first pair's branches",
			src: "package db\n\nfunc q(a bool) string {\n" +
				"\tq := \"SELECT id FROM t\"\n" +
				"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
				"\tif !a {\n\t\tq += \" LIMIT 1\"\n\t}\n" +
				"\tb := a\n" +
				"\tif b {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
				"\tif !b {\n\t\tq += \" LIMIT 2\"\n\t}\n" +
				"\treturn q\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			texts := foldTexts(t, tt.src)
			if impossible := "SELECT id FROM t LIMIT 1 FOR UPDATE"; hasFoldText(texts, impossible) {
				t.Errorf("several pairs must not multiply out into a mixed world: %q in %q", impossible, texts)
			}
		})
	}
}

// LOSING A PROOF MUST WIDEN THE EMISSION, NEVER SWAP IT. When a pair cannot be
// proven, all four combinations of its two branches are reachable, so the
// one-branch worlds must be emitted ALONGSIDE full and base. Dropping the pair
// back to full/base alone is the failure that matters in a lockdown gate: here
// the o.Ordered=true path is `… FOR UPDATE` with no ORDER BY, and a benign
// helper call handed the options struct is all it takes to lose it.
func TestFromGoReassembledUnprovablePairStillEmitsBranchWorlds(t *testing.T) {
	src := "package db\n\ntype opt struct{ Ordered bool }\n\nfunc col(o *opt) string { return \"id\" }\n\n" +
		"func q(o *opt) string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\tif !o.Ordered {\n\t\tq += \" ORDER BY \" + col(o)\n\t}\n" +
		"\tif o.Ordered {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	if want := "SELECT id FROM t FOR UPDATE"; !hasFoldText(texts, want) {
		t.Fatalf("the o.Ordered=true world is reachable and unordered: %q not in %q", want, texts)
	}
}

// The same rule for a closure: `reset` makes the pair unprovable, and the x=true
// world is a locking SELECT with no ORDER BY. Suppressing the pair must not take
// that world with it — base here is the bare seed and carries no lock at all, so
// "the drop restores base" is no answer.
func TestFromGoReassembledClosureUnprovablePairStillEmitsBranchWorlds(t *testing.T) {
	src := "package db\n\nfunc q(x bool) string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\treset := func() {\n\t\tx = false\n\t}\n" +
		"\tif !x {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif x {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\treset()\n\treturn q\n}\n"
	texts := foldTexts(t, src)
	if want := "SELECT id FROM t FOR UPDATE"; !hasFoldText(texts, want) {
		t.Fatalf("the x=true world is reachable and unordered: %q not in %q", want, texts)
	}
}

// `*o = opt{}` overwrites everything the guard reads, exactly as `o.Ordered =
// false` does — the write just does not name the field. Seeing it is what makes
// the pair unprovable, and an unprovable pair WIDENS: all four combinations of
// the two branches are emitted, the lock-only world among them.
//
// In this particular source that world is not in fact reachable (the reset
// forces the second branch), so the fold emits a world no path produces. That is
// the trade the narrowed model takes deliberately — the alternative, ruling it
// out, needs the ordering analysis three review rounds could not make sound, and
// its failures were silence rather than a visible finding.
func TestFromGoReassembledGuardOverwrittenThroughPointerWidens(t *testing.T) {
	src := "package db\n\ntype opt struct{ Ordered bool }\n\nfunc q(o *opt) string {\n" +
		"\tq := \"SELECT id FROM t WHERE status = 'x'\"\n" +
		"\tif o.Ordered {\n\t\tq += \" FOR UPDATE\"\n\t}\n" +
		"\t*o = opt{}\n" +
		"\tif !o.Ordered {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\treturn q\n}\n"
	texts := foldTexts(t, src)
	for _, want := range []string{
		"SELECT id FROM t WHERE status = 'x' FOR UPDATE",             // o.Ordered true at the first read
		"SELECT id FROM t WHERE status = 'x' ORDER BY id",            // false at the first read
		"SELECT id FROM t WHERE status = 'x' FOR UPDATE ORDER BY id", // true, then reset
	} {
		if !hasFoldText(texts, want) {
			t.Errorf("an unprovable pair widens to every combination: %q not in %q", want, texts)
		}
	}
}

// Emission is what the rule PARSES: every fold text is handed to pg_query on
// wazero, and those parses dominate the rule's runtime (which is why the
// pre-parse gate exists). The model is bounded per variable by construction:
// full, base, and the branch worlds of at most maxEnumeratedPairs pairs, each
// with a minimal and a maximal rendering — 2 + 4×3 = 14.
//
// The bound is LINEAR in the pair cap and independent of the source: five flags
// here, three pairs enumerated, and the two independent optionals (d, e) add no
// worlds of their own — they ride the all-or-nothing bound. What this rules out
// is an enumeration whose exponent depends on the source, which is what a
// cross-product over pairs would have been.
func TestFromGoReassembledEmissionIsBoundedPerVariable(t *testing.T) {
	seed := "SELECT id FROM t"
	src := "package db\n\nfunc q(a, b, c, d, e bool) string {\n" +
		"\tq := \"SELECT id FROM t\"\n" +
		"\tif a {\n\t\tq += \" AND a1\"\n\t}\n" +
		"\tif !a {\n\t\tq += \" AND a2\"\n\t}\n" +
		"\tif b {\n\t\tq += \" AND b1\"\n\t}\n" +
		"\tif !b {\n\t\tq += \" AND b2\"\n\t}\n" +
		"\tif c {\n\t\tq += \" AND c1\"\n\t}\n" +
		"\tif !c {\n\t\tq += \" AND c2\"\n\t}\n" +
		"\tif d {\n\t\tq += \" ORDER BY id\"\n\t}\n" +
		"\tif e {\n\t\tq += \" LIMIT 1\"\n\t}\n" +
		"\tq += \" FOR UPDATE\"\n\treturn q\n}\n"
	if folds := foldOnly(foldTexts(t, src), seed); len(folds) > 14 {
		t.Errorf("emission must stay bounded per variable: %d fold texts: %q", len(folds), folds)
	}
}
