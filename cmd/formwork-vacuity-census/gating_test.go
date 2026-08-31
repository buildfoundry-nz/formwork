package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The census gates four classes and reports six. Every gated class reports 0
// while every measured class carries a population, so the run ends
// "OK: every rule can fail" with ~970 instances on the ungated side (#12178).
// These tests pin the promotion of each measured class to a FAILURE, and the
// two probe defects that kept the gated side empty.

// writeCorpus builds a throwaway repo root: .formwork/formwork.yaml, one rule
// file, and the source files the rules are pointed at. Rule count stays under
// minCorpusForCanaries so the named-canary and registry arms stay quiet.
func writeCorpus(t *testing.T, rules string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	mkdir := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(root, ".formwork", "formwork.yaml")
	mkdir(cfg)
	if err := os.WriteFile(cfg, []byte("version: 1\nlanes:\n  always: { tags: [always] }\n  ci: { all: true, ci: true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rf := filepath.Join(root, ".formwork", "rules", "test.yaml")
	mkdir(rf)
	if err := os.WriteFile(rf, []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	for p, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(p))
		mkdir(abs)
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// writeFixture plants one fixture tree for a rule: .formwork/fixtures/<rule>/<dir>/…
// A synthetic corpus stays under minCorpusForCanaries, so the fixture REGISTRY
// arms stay quiet and only the fire/pass behaviour under test is measured.
func writeFixture(t *testing.T, root, ruleID, dir string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		abs := filepath.Join(root, ".formwork", "fixtures", ruleID, dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// census runs the real entry point over root and returns exit code + output.
func census(t *testing.T, root string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(root, true, false, &out, &errb)
	return code, out.String() + errb.String()
}

// A scope.exclude naming a LITERAL path that does not exist is currently
// printed under "measured (not gated)" and the census still exits 0. It must
// fail: a carve-out for one named file is a statement about that file, and when
// the file is gone the statement is stale drift.
func TestDeadExcludeGlobGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-exclude
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/nowhere/gone.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-EXCLUDE-GLOB") {
		t.Fatalf("no DEAD-EXCLUDE-GLOB verdict in output:\n%s", out)
	}
	if strings.Contains(out, "measured (not gated)") {
		t.Fatalf("dead exclude still reported as measured-not-gated:\n%s", out)
	}
}

// A dead except.paths glob is the same claim one key over.
func TestDeadExceptGlobGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths: ['src/nowhere/gone.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-EXCEPT-GLOB") {
		t.Fatalf("no DEAD-EXCEPT-GLOB verdict in output:\n%s", out)
	}
	if strings.Contains(out, "measured (not gated)") {
		t.Fatalf("dead except.paths still reported as measured-not-gated:\n%s", out)
	}
}

// An exclude glob naming a tree the ENGINE prunes before rules run (.formwork,
// .git) names something real: formwork-engine-skip-declared requires exactly
// that declaration on every content rule whose include reaches a .formwork
// path. Measuring it against the post-skip walk reports it dead, which would
// make the two rules demand opposite things. It must be measured against the
// tree that includes the built-in-skip subtree, so it counts as LIVE.
func TestExcludeGlobOverBuiltinSkipDirIsLive(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: skip-declared
    type: forbidden-pattern
    severity: error
    scope:
      include: ['**/*.go']
      exclude: ['.formwork/**']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	// A file under the engine's built-in skip dir — never walked by scan.Walk.
	if err := os.WriteFile(filepath.Join(root, ".formwork", "fixtures.go"), []byte("package f\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-EXCLUDE-GLOB") {
		t.Fatalf("an exclude over the engine's built-in skip dir was reported dead:\n%s", out)
	}
}

// COMMENT-SUFFICIENT is gating only for Go today (curableLang), so the 15 live
// findings — 11 of them workflow-scoped YAML — are reported and never enforced.
// The census's premise is half right and half wrong. RIGHT: formwork carries no
// decomment-* projection for YAML — the registry is code-only-dart,
// comments-only-{awk,dart,go,sql}, decomment-{go,sh}, decomment-destring-go,
// destring-{sh,decomment-sh}, strings-only-{go,sh}, raw, and nothing else
// (verified against formwork's internal/preprocess/*.go). WRONG: the
// conclusion "no cure available yet". A cure that needs no projection is
// already in use by 22 rules in this corpus — the `^[^#]*` comment-immunity
// anchor. So the verdict must gate in every language, and the note must name
// the anchor instead of claiming there is no cure.
func TestCommentSufficientGatesInYaml(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: yaml-comment-satisfied
    type: required-pattern
    severity: error
    scope:
      include: ['.github/workflows/**']
    params: { mode: exists, pattern: 'run-the-gate' }
    tags: [always]
`, map[string]string{
		".github/workflows/ci.yml": "# the run-the-gate step below is load-bearing\njobs:\n  a:\n    steps:\n      - run: run-the-gate\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "COMMENT-SUFFICIENT") {
		t.Fatalf("no COMMENT-SUFFICIENT verdict in output:\n%s", out)
	}
	if strings.Contains(out, "no cure available yet") {
		t.Fatalf("the false 'no cure available yet' conclusion is still printed:\n%s", out)
	}
	if !strings.Contains(out, `^[^#]*`) {
		t.Fatalf("the note does not name the ^[^#]* anchor as the cure:\n%s", out)
	}
}

// DETECTOR-SATISFIED is documented in main.go's header and never implemented:
// a rule whose only witness is its own detector asserts that the detector still
// contains some text, not that the detector's invariant holds.
func TestDetectorSatisfiedIsImplementedAndGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: detector-satisfied
    type: required-pattern
    severity: error
    scope:
      include: ['scripts/dev/**', 'api-factory/**/*.go']
    params: { mode: exists, pattern: 'MarkActiveBoqStaleInTx' }
    tags: [always]
`, map[string]string{
		"scripts/dev/check-boq-stale.go":    "package main\n\nconst want = \"MarkActiveBoqStaleInTx\"\n",
		"api-factory/routes/boq/handler.go": "package boq\n\nfunc H() {}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DETECTOR-SATISFIED") {
		t.Fatalf("no DETECTOR-SATISFIED verdict in output:\n%s", out)
	}
}

// isExistenceObligation admits only required-pattern(exists) and
// pattern-count(at-least), so set-relation and pair-consistency never reach a
// class-2 probe at all. The sound set-relation verdict is the one experiment
// this instrument can decide outright: with the whole required side blanked,
// does the relation still hold? Here it does — `src/routes/**` carries no
// `route:` element, so `A ⊆ B` is satisfied over EVERY tree, including one
// where the required side does not exist.
//
// The sides are declared over separate globs on purpose. That is the condition
// under which blanking b is a probe: when the two sides draw from the same
// files, A empties along with B and the pass says nothing about the tree.
func TestRelationWithAnInertRequiredSideGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: name-only-join
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      a: { files: ['src/routes/**'], pattern: 'route:([a-z]+)', group: 1 }
      b: { files: ['src/tests/**'], pattern: 'route:([a-z]+)', group: 1 }
      relation: subset
    tags: [always]
`, map[string]string{
		"src/routes/a.go":     "package a\n\nfunc A() {}\n",
		"src/tests/a_test.go": "package a\n// route:alpha\n",
		"src/tests/b_test.go": "package a\n// route:alpha\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "INERT-REQUIRED-SIDE") {
		t.Fatalf("no INERT-REQUIRED-SIDE verdict in output:\n%s", out)
	}
}

// The converse of the same fixture, and the reason the binary search had to go.
// Give `src/routes/**` a real element and the required side becomes
// load-bearing: blanking it empties B and `A ⊆ B` fails. Two b-side files each
// carry the element, so NO SINGLE file is load-bearing — which the withdrawn
// search called a defect. Redundant coverage is the relation holding, not the
// relation being unfalsifiable, and the census must stay quiet here.
func TestRelationWithARedundantlyCoveredRequiredSideIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: redundant-join
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      a: { files: ['src/routes/**'], pattern: 'route:([a-z]+)', group: 1 }
      b: { files: ['src/tests/**'], pattern: 'route:([a-z]+)', group: 1 }
      relation: subset
    tags: [always]
`, map[string]string{
		"src/routes/a.go":     "package a\n// route:alpha\n",
		"src/tests/a_test.go": "package a\n// route:alpha\n",
		"src/tests/b_test.go": "package a\n// route:alpha\n",
	})

	if _, out := census(t, root); strings.Contains(out, "INERT-REQUIRED-SIDE") {
		t.Fatalf("a redundantly covered relation was reported vacuous:\n%s", out)
	}
}

// The report's closing summary must not carry a "measured (not gated)" section
// at all any more: every class the census can detect is now an offence.
func TestNoMeasuredNotGatedSectionRemains(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: clean
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})

	_, out := census(t, root)
	if strings.Contains(out, "measured (not gated)") {
		t.Fatalf("a measured-not-gated section survives:\n%s", out)
	}
}
