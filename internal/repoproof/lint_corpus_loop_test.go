// lint_corpus_loop_test.go — #284.
//
// #89's deliverable was that `formwork lint` runs over the examples/ corpora,
// not only at the repo root. It was delivered as a loop inside the Makefile's
// `lint:` recipe, and nothing asserted the loop was there.
//
// That is the #89 defect one level up. Reduce the recipe to its root-only
// invocation — the exact shape #89 was filed against — and `go test ./...`
// stays fully green: the corpora go unlinted again, the four checks #89 named
// stop running over the 614-rule corpus, and the only signal is a `make lint`
// that got quieter. A deliverable whose removal reddens nothing is not held.
//
// The corpus stream could not write this: the pin has to read the Makefile,
// and internal/repoproof is the package that already does that (gate_test.go,
// make_targets_test.go, help_completeness_test.go all parse recipes here).
package repoproof_test

import (
	"strings"
	"testing"
)

// TestLintRecipeStillLoopsTheCorpora pins #89's deliverable to the recipe that
// implements it.
//
// Asserted as three separate properties rather than one string compare, so a
// legitimate reshaping of the recipe (a different shell idiom, an added echo)
// does not fail while a removal of the loop does:
//
//   - it iterates examples/
//   - each iteration invokes formwork lint against that directory
//   - a failing corpus fails the target, rather than the loop swallowing it
//
// The third is the one most easily lost: `for d in …; do cmd; done` exits with
// the status of the LAST iteration, so a corpus that fails anywhere but last
// reads as a pass. The recipe carries an explicit fail accumulator and this
// holds it there.
func TestLintRecipeStillLoopsTheCorpora(t *testing.T) {
	recipe, ok := recipes(readMakefile(t))["lint"]
	if !ok {
		t.Fatal("the Makefile has no `lint:` target — #89's deliverable is the examples/ loop inside it")
	}

	if !strings.Contains(recipe, "examples/") {
		t.Errorf("`lint:` no longer names examples/ — #89's whole deliverable is that lint runs over "+
			"the corpora, not just the repo root, and nothing else in the tree asserts it does.\nrecipe:\n%s", recipe)
	}

	// The loop body must actually lint the directory it is iterating, not just
	// echo it. `-C "$d"` is what makes the corpus the subject.
	if !strings.Contains(recipe, "lint -C") {
		t.Errorf("`lint:` iterates examples/ but never runs `formwork lint -C` against each corpus, "+
			"so the loop reports on nothing.\nrecipe:\n%s", recipe)
	}

	// A `for` loop exits with the last iteration's status. Without an
	// accumulator a corpus that fails anywhere but last is invisible.
	if !strings.Contains(recipe, "fail=1") || !strings.Contains(recipe, "exit $$fail") {
		t.Errorf("`lint:`'s corpus loop has no failure accumulator, so a corpus that fails anywhere "+
			"but LAST exits 0 — the loop takes the status of its final iteration.\nrecipe:\n%s", recipe)
	}
}
