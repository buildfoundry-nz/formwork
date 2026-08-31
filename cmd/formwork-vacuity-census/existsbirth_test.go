package main

import (
	"strings"
	"testing"
)

// The second arm on this detector (#15848): a NEW required-pattern arm may not
// combine `mode: exists` with a scope that can be satisfied by a file the author
// did not mean.
//
// `mode: exists` asks whether the pattern appears ANYWHERE in scope. Over a
// single file that is the same question as `every-file`. Over a scope of two
// hundred, one incidental hit anywhere satisfies the obligation for all of them,
// and deleting the real thing is masked by a coincidence in a file nobody was
// thinking about. #9992 diagnosed the class in July: four instances were
// repaired by hand — all four by MOVING TYPE — and the comparable set has since
// grown from 54 to 63.
//
// WHY THIS IS A BIRTH RATCHET AND NOTHING ELSE. sw-inert2 measured the
// retrospective form and correctly abandoned it: the only predicate with full
// recall against the known instances fires 63 times, and the two gateable
// sub-shapes are disjoint because one is a per-unit obligation and the other a
// global one — a distinction of INTENT, invisible in the text. So no gate can
// sort the standing 63, and the only form that stops the class regrowing is one
// that judges arms as they are written.
//
// WHY THE PREDICATE IS TEXT ONLY. sw-inert2's recommendation was the union of
// "more than one file matched" and "at least one wildcard glob". The first half
// is a property of the rule AND THE TREE, and this arm judges a transition
// between two commits: a rule whose scope drifts from one file to two with no
// edit by anyone would then fire on whoever pushed next, which is collateral on
// an author who did nothing. The wildcard half is a property of the rule text
// alone, stable across a refactor that adds or deletes files — which is what a
// birth gate needs, as sw-inert2 argued themselves. Taking `declared-glob-count
// > 1` in place of `files-matched > 1` keeps the measured coverage without the
// tree dependency: it catches the two-literal-globs spelling
// (annotated-pdf-export-cap-fail-loud, upload-pick-declares-busy) that no
// wildcard test sees, and the seven wildcard-but-one-file-today arms that a
// file-count predicate is blind to at birth are caught by the wildcard half.
//
// WHY THE ESCAPE IS LOAD-BEARING, AND WHY IT IS NOT CHECKED. Measured over the
// standing set, a material and unquantified share of these arms are existential
// BY DESIGN — "X has
// a live caller", "X has a consumer somewhere in the frontend", "X is wired".
// For those the multi-file scope IS the invariant and narrowing it would assert
// something the author does not mean. A gate that refuses them outright is one
// that gets switched off, so the declaration is not a nicety. It is also NOT a
// checkable predicate: at birth nothing in the text distinguishes a per-unit
// obligation from a global one, which is the same reason no retrospective gate
// exists. The declaration is a REVIEWED escape and the rule header says so
// rather than implying the gate validates the reason.

// ebMultiGlobExists is the shape: `mode: exists` over a wildcard scope. One
// incidental hit anywhere under src/ satisfies it for every file there.
const ebMultiGlobExists = `rules:
  - id: eb-wide-exists
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      pattern: 'REQUIRED_TOKEN'
      mode: exists
    cure: "Narrow the scope."
    tags: [always]
`

// ebSingleFileExists is the same arm scoped to ONE file. Over a single file
// `exists` and `every-file` are the same question, so there is nothing to refuse.
const ebSingleFileExists = `rules:
  - id: eb-narrow-exists
    type: required-pattern
    severity: error
    scope:
      include: ['src/base.go']
    params:
      pattern: 'REQUIRED_TOKEN'
      mode: exists
    cure: "Narrow the scope."
    tags: [always]
`

// ebTwoLiteralGlobs carries no wildcard at all — two literal globs. This is the
// spelling a wildcard-only predicate misses, and sw-inert2 measured two live
// instances of it.
const ebTwoLiteralGlobs = `rules:
  - id: eb-two-literals-exists
    type: required-pattern
    severity: error
    scope:
      include: ['src/base.go', 'gen/gen.go']
    params:
      pattern: 'REQUIRED_TOKEN'
      mode: exists
    cure: "Narrow the scope."
    tags: [always]
`

// ebDeclaredExists is the reviewed escape: the same wide arm carrying the
// in-place declaration. The gate does not validate the reason and must not
// pretend to — it records that a human wrote one.
const ebDeclaredExists = `rules:
  # exists-multi-file: every takeoff surface must have a live caller SOMEWHERE;
  # narrowing this to one file would assert something the rule does not mean.
  - id: eb-declared-exists
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      pattern: 'REQUIRED_TOKEN'
      mode: exists
    cure: "Narrow the scope."
    tags: [always]
`

// ebEveryFile is the same scope WITHOUT mode: exists. The default per-file mode
// obliges every file in scope, so a wide scope is not a defect there — and an
// arm that fired on it would refuse most of the corpus.
const ebEveryFile = `rules:
  - id: eb-every-file
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      pattern: 'REQUIRED_TOKEN'
    cure: "Add the token."
    tags: [always]
`

// ebSources satisfy every arm above, so the rules PASS and the only thing that
// can speak about them is the birth arm.
func ebSources() map[string]string {
	return map[string]string{
		"src/base.go": "package a\n\n// REQUIRED_TOKEN\nconst Alpha = 1\n",
		"gen/gen.go":  "package gen\n\n// REQUIRED_TOKEN\nconst Alpha = 1\n",
	}
}

// ebVerdict is the rendered finding, never the bare code — same reason as the
// first arm: report()'s FAIL prose names the code and prints on ANY failure.
const ebVerdict = " [NEW-EXISTS-ARM-UNDECLARED] "

// A new exists arm over a wildcard scope is refused, and the message must say
// what to do about it.
func TestNewWideExistsArmIsRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", ebMultiGlobExists)
	commitAll(t, root, "add a wide exists arm")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	code, out := census(t, root)
	if !strings.Contains(out, ebVerdict) {
		t.Fatalf("a new wide exists arm was not refused:\n%s", out)
	}
	if !strings.Contains(out, "eb-wide-exists") {
		t.Fatalf("the verdict does not name the arm it refuses:\n%s", out)
	}
	if code == 0 {
		t.Fatalf("census exited 0 with a gating verdict:\n%s", out)
	}
}

// The two-literal-globs spelling, which a wildcard-only predicate misses and
// which sw-inert2 measured twice in the live corpus.
func TestNewExistsArmOverTwoLiteralGlobsIsRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", ebTwoLiteralGlobs)
	commitAll(t, root, "add an exists arm over two literal globs")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if !strings.Contains(out, ebVerdict) {
		t.Fatalf("a multi-glob exists arm with no wildcard was not refused:\n%s", out)
	}
	if !strings.Contains(out, "eb-two-literals-exists") {
		t.Fatalf("the verdict does not name the arm it refuses:\n%s", out)
	}
}

// Over ONE file, exists and every-file are the same question. Refusing this
// would be refusing the cure the gate itself recommends.
func TestNewSingleFileExistsArmIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", ebSingleFileExists)
	commitAll(t, root, "add a narrow exists arm")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, ebVerdict) {
		t.Fatalf("a single-file exists arm was refused — that IS the cure:\n%s", out)
	}
}

// A wide scope without mode: exists is the ordinary per-file obligation. An arm
// that fired here would refuse most of the corpus.
func TestNewWideEveryFileArmIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", ebEveryFile)
	commitAll(t, root, "add a wide every-file arm")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, ebVerdict) {
		t.Fatalf("a wide every-file arm was refused:\n%s", out)
	}
}

// The reviewed escape. A material, deliberately unquantified share of the
// standing set is existential by design, so without this the gate refuses arms
// whose multi-file scope IS the
// invariant, and a gate that refuses correct work gets switched off.
func TestNewWideExistsArmWithADeclarationIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	gitInit(t, root)
	base := commitAll(t, root, "base")

	writeRules(t, root, "new.yaml", ebDeclaredExists)
	commitAll(t, root, "add a declared wide exists arm")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if strings.Contains(out, ebVerdict) {
		t.Fatalf("a DECLARED wide exists arm was refused:\n%s", out)
	}
}

// The standing 63 are excluded by construction, not by a list. This is the same
// property the first arm has and the reason neither needs a grandfather file:
// the exemption is the diff itself, so there is no baseline and therefore no
// regenerator that could silently grow one.
func TestAStandingWideExistsArmIsNotRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	writeRules(t, root, "old.yaml", ebMultiGlobExists)
	gitInit(t, root)
	base := commitAll(t, root, "base already holds the wide exists arm")

	writeRules(t, root, "unrelated.yaml", ebSingleFileExists)
	commitAll(t, root, "an unrelated addition")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	// Asserted on the RENDERED verdict, not the bare rule id. The census prints
	// a classification table naming EVERY rule in the corpus, so
	// `!Contains(out, "eb-wide-exists")` can never hold and the test would fail
	// whatever the arm did. The RED run caught this — it is the same false
	// assertion the first arm's tests had, one shape over: an assertion that
	// cannot hold and an assertion that is violated look identical in a red run,
	// which is exactly why every assertion here names the verdict.
	_, out := census(t, root)
	if strings.Contains(out, ebVerdict) {
		t.Fatalf("a standing arm was charged to an author who did not write it:\n%s", out)
	}
}

// And the edit hole, closed the same way the first arm closes it: narrowing to
// one file then widening back is not an addition, and a gate that missed it
// would be trivially walked around.
func TestWideningAnExistsArmIntoTheClassIsRefused(t *testing.T) {
	root := writeCorpus(t, baseRules, ebSources())
	writeRules(t, root, "arm.yaml", ebSingleFileExists)
	gitInit(t, root)
	base := commitAll(t, root, "base holds a NARROW exists arm")

	writeRules(t, root, "arm.yaml",
		strings.Replace(ebSingleFileExists, "['src/base.go']", "['src/**/*.go']", 1))
	commitAll(t, root, "widen the scope")
	t.Setenv("TDD_TWO_COMMIT_SPLIT_RANGE", base+"..HEAD")

	_, out := census(t, root)
	if !strings.Contains(out, ebVerdict) {
		t.Fatalf("an exists arm WIDENED into the class was not refused:\n%s", out)
	}
	if !strings.Contains(out, "eb-narrow-exists") {
		t.Fatalf("the verdict does not name the arm it refuses:\n%s", out)
	}
}

// existsBirthReason is the predicate itself, tested directly so the shape is
// pinned independently of the census's rendering.
func TestExistsBirthReasonIsTextOnly(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		globs   []string
		declare bool
		want    bool
	}{
		{"wildcard exists", "exists", []string{"src/**/*.go"}, false, true},
		{"two literal globs exists", "exists", []string{"a.go", "b.go"}, false, true},
		{"single literal exists", "exists", []string{"a.go"}, false, false},
		{"wildcard every-file", "", []string{"src/**/*.go"}, false, false},
		{"wildcard exists declared", "exists", []string{"src/**/*.go"}, true, false},
		{"char class counts as wildcard", "exists", []string{"src/file[0-9].go"}, false, true},
		{"question mark counts as wildcard", "exists", []string{"src/file?.go"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := existsBirthReason(c.mode, c.globs, c.declare) != ""
			if got != c.want {
				t.Fatalf("existsBirthReason(mode=%q, globs=%v, declared=%v) fired=%v, want %v",
					c.mode, c.globs, c.declare, got, c.want)
			}
		})
	}
}
