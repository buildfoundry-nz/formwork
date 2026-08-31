package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// #15986 (epic #14875) — the census asks of a FIRE fixture whether it still
// trips its rule, and of a PASS fixture whether it has started tripping it. It
// never asks of a PASS fixture whether it COULD have tripped it.
//
// fixtureVerdicts counts in-scope files inside `if isFire` and nowhere else.
// A pass fixture with no file in the rule's scope is green whatever the rule
// says, and the pair reports as discriminating. Measured on the corpus at
// e28bb4eb33: 113 of 2468 pass fixtures have zero in-scope files.
//
// Most of those 113 are RIGHT. Parking a real violation at an excluded path is
// how an exclusion glob is held in place, and deleting the glob is what such a
// fixture exists to catch. So the arm cannot be "no in-scope file"; it has to
// be "no in-scope file AND nothing here the rule would have judged".
//
// The second half is decided by the ENGINE, never by a re-implemented matcher:
// the probe rebuilds the rule with a universal scope and re-runs it over the
// same tree. That is what makes the polarity fall out for free instead of
// being hand-coded — a ban is violated by a match, a required-pattern by the
// ABSENCE of one, and engine.Run already knows which is which. Re-implementing
// the matcher here is the exact error that produced the false "133 of 200 rules
// match zero files" result the census exists to correct (#10083).
// ---------------------------------------------------------------------------

// The class. A ban's clean fixture sits at an excluded path AND carries no
// banned text: delete the exclusion glob and it is still green, so it pins
// neither the glob nor the pattern.
func TestPassFixtureWithNothingTheRuleWouldJudgeGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: inert-pass
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/generated/**']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	writeFixture(t, root, "inert-pass", "fire-1", map[string]string{
		"src/a.go": "package a\n\nvar x = BANNED\n",
	})
	writeFixture(t, root, "inert-pass", "pass-1", map[string]string{
		"src/generated/g.go": "package generated\n\nvar y = fine\n",
	})

	code, out := census(t, root)
	if !strings.Contains(out, "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("no PASS-FIXTURE-VACUOUS verdict in output:\n%s", out)
	}
	if code == 0 {
		t.Fatalf("census exited 0 on an inert pass fixture; the verdict must gate:\n%s", out)
	}
}

// The 95 that are RIGHT, and the reason the arm cannot be "no in-scope file".
// Same excluded path, but the specimen carries the banned text. Delete the
// exclusion glob and this fixture goes red — that is exactly what it is for.
func TestPassFixtureParkingARealViolationAtAnExcludedPathIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: scope-test-pass
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['src/generated/**']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	writeFixture(t, root, "scope-test-pass", "fire-1", map[string]string{
		"src/a.go": "package a\n\nvar x = BANNED\n",
	})
	writeFixture(t, root, "scope-test-pass", "pass-1", map[string]string{
		"src/generated/g.go": "package generated\n\nvar y = BANNED\n",
	})

	if _, out := census(t, root); strings.Contains(out, "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("a pass fixture holding an exclusion glob in place was called vacuous:\n%s", out)
	}
}

// POLARITY, and the reason a re-implemented matcher gets this wrong. For a
// required-pattern the violation is the pattern's ABSENCE, so an out-of-scope
// file that legitimately LACKS it is the meaningful scope test — it is what
// holds the include glob's suffix in place against scope creep. This is
// desk-card-composes-frame/pass-2's shape, and the naive "does the fixture
// contain the pattern" probe accused it.
func TestOutOfScopeFileLackingARequiredPatternIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: suffix-anchored-requirement
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*_card.go']
    params: { pattern: 'CardFrame\(', mode: every-file }
    tags: [always]
`, map[string]string{"src/a_card.go": "package a\n\nvar w = CardFrame()\n"})
	writeFixture(t, root, "suffix-anchored-requirement", "fire-1", map[string]string{
		"src/b_card.go": "package b\n\nvar w = handRolled()\n",
	})
	// Not a card, so not in scope, and it builds no frame. A scope that crept
	// to src/**/*.go would fire here; that is the assertion.
	writeFixture(t, root, "suffix-anchored-requirement", "pass-1", map[string]string{
		"src/b_row.go": "package b\n\nvar w = handRolled()\n",
	})

	if _, out := census(t, root); strings.Contains(out, "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("an out-of-scope file lacking a required pattern was called vacuous:\n%s", out)
	}
}

// The ordinary case must stay silent: a pass fixture the rule actually reaches
// is doing its job whatever its content.
func TestPassFixtureWithInScopeFilesIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: ordinary-pass
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	writeFixture(t, root, "ordinary-pass", "fire-1", map[string]string{
		"src/a.go": "package a\n\nvar x = BANNED\n",
	})
	writeFixture(t, root, "ordinary-pass", "pass-1", map[string]string{
		"src/a.go": "package a\n\nvar y = fine\n",
	})

	if _, out := census(t, root); strings.Contains(out, "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("a pass fixture the rule reaches was called vacuous:\n%s", out)
	}
}

// A DECLARED inert except.paths entry does not make its fixture an offence.
//
// `# except-declaration: <reason>` (#10777) is the corpus's existing vocabulary
// for "this except.paths entry names a file the pattern could never match
// anyway, and it is listed so the roster stays complete and auditable". Where
// an author has written that reasoning down, a pass fixture standing at that
// path is inert BY DECLARATION, and an arm that fires on it is demanding they
// either delete an auditable roster entry or fabricate a violation to justify
// keeping it. That is how an arm gets switched off in its first week —
// labour-method-choice-single-writer/pass-3 is exactly this shape on the real
// corpus, and its rationale is spelled out in the rule file.
//
// The declaration must carry a non-empty reason, matching the convention
// `# glob-dead:` and `# source-list-exhaustive:` already follow.
func TestDeclaredInertExceptPathIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: declared-inert-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths:
        # except-declaration: generic writer, parameterised, never carries the literal
        - 'src/generic/param.go'
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	writeFixture(t, root, "declared-inert-except", "fire-1", map[string]string{
		"src/a.go": "package a\n\nvar x = BANNED\n",
	})
	writeFixture(t, root, "declared-inert-except", "pass-1", map[string]string{
		"src/generic/param.go": "package generic\n\nvar y = bind(kind)\n",
	})

	if _, out := census(t, root); strings.Contains(out, "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("a pass fixture at a DECLARED inert except.paths entry was called vacuous:\n%s", out)
	}
}

// The declaration must be real. A bare marker with no reason declares nothing
// and must not buy silence — same rule the sibling vocabularies enforce.
func TestBareExceptDeclarationDoesNotExcuseAVacuousPassFixture(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: bare-decl-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths:
        # except-declaration:
        - 'src/generic/param.go'
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	writeFixture(t, root, "bare-decl-except", "fire-1", map[string]string{
		"src/a.go": "package a\n\nvar x = BANNED\n",
	})
	writeFixture(t, root, "bare-decl-except", "pass-1", map[string]string{
		"src/generic/param.go": "package generic\n\nvar y = bind(kind)\n",
	})

	if _, out := census(t, root); !strings.Contains(out, "PASS-FIXTURE-VACUOUS") {
		t.Fatalf("a bare `# except-declaration:` with no reason bought silence:\n%s", out)
	}
}
