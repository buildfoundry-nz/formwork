package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #10777 — the second way an except.paths entry stops excusing anything.
//
// #12178 gated the entry whose path is GONE (DEAD-EXCEPT-GLOB). The entry whose
// path is still THERE, still in scope, and which the rule no longer fires on is
// the same rot one step later, and nothing sees it: except.paths is a scope
// SUBTRACTION (config.Rule.Applies returns false), so the rule never evaluates
// the file, no finding is ever suppressed, and every instrument reads the entry
// as a live, accounted-for escape hatch forever. That is how
// dart-measure-active-step kept claiming measure_type_filter_providers.dart as
// the home of a resolver #10346 had turned into a projection — found by hand,
// by running the rule's own regex across two revisions.
//
// The cost is governance, not tidiness: once the list no longer says which
// entries are load-bearing, an honest amendment to an ownership manifest is
// indistinguishable from an allowlist widening.

// A literal except.paths entry that names a file the rule does not fire on
// excuses nothing. It must gate, naming the rule and the path.
func TestInertExceptPathGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: inert-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths: ['src/decl.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{
		"src/a.go":    "package a\n",
		"src/decl.go": "package decl\n\nconst Name = \"decl\"\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "INERT-EXCEPT") {
		t.Fatalf("no INERT-EXCEPT verdict for an entry the rule cannot fire on:\n%s", out)
	}
	if !strings.Contains(out, "inert-except") || !strings.Contains(out, "src/decl.go") {
		t.Fatalf("the verdict does not name the rule and the path:\n%s", out)
	}
	// The three shapes carry three cures, so each is asserted on its own wording
	// rather than on the shared code. A verdict that named the wrong one would
	// send a reader to the wrong fix while this test stayed green.
	if !strings.Contains(out, "present and in the rule's scope") {
		t.Fatalf("the in-scope shape did not use its own message:\n%s", out)
	}
}

// The second shape: the path exists, but the rule's scope never contained it,
// so the exception has nothing to subtract. Distinct cure — fix the scope it
// was written against — so it must not borrow the in-scope wording.
func TestExceptPathOutsideScopeIsInert(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: out-of-scope-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths: ['docs/notes.md']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{
		"src/a.go":      "package a\n",
		"docs/notes.md": "# notes\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "INERT-EXCEPT") {
		t.Fatalf("an except.paths entry outside the rule's scope was not reported:\n%s", out)
	}
	if !strings.Contains(out, "never contained") {
		t.Fatalf("the out-of-scope shape did not use its own message:\n%s", out)
	}
}

// The third shape: the path is real — measured against the walk that KEEPS the
// engine's built-in skip, so it counts as live — but scan.Walk drops .git and
// .formwork before any rule runs, so no rule was ever going to fire on it. The
// entry cannot excuse anything and no edit to the rule or the file changes that.
func TestExceptPathUnderBuiltinSkipIsInert(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: skipped-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['**/*.go']
    except:
      paths: ['.formwork/planted.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})
	// Under the engine's built-in skip dir — real on disk, never walked.
	if err := os.WriteFile(filepath.Join(root, ".formwork", "planted.go"), []byte("package f\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, out := census(t, root)
	if !strings.Contains(out, "INERT-EXCEPT") {
		t.Fatalf("an except.paths entry the engine never reads was not reported:\n%s", out)
	}
	if !strings.Contains(out, "built-in walk skip") {
		t.Fatalf("the built-in-skip shape did not use its own message:\n%s", out)
	}
}

// A hole that goes unnamed reads as coverage — the defect this census exists to
// catch, one level up. A one-file probe cannot judge a relation (satisfied by an
// empty tree) and will not shell out per entry for a heavy rule, so those
// entries get NO verdict. main.go already says this out loud for the
// set-relation rules the blank-the-b-side experiment cannot answer; the same
// obligation applies here, or the arm's zero claims a coverage it does not have.
func TestUnjudgedExceptEntriesAreReported(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: relation-with-except
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths: ['src/decl.go']
    params:
      where: same-func
      trigger: 'alpha'
      requires: 'Memo\('
    tags: [always]
`, map[string]string{
		"src/a.go":    "package a\n\nfunc Load() {\n\talpha\n\tMemo()\n}\n",
		"src/decl.go": "package decl\n\nconst Name = \"decl\"\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "except.paths entries NOT decided here") {
		t.Fatalf("an except.paths entry no probe can judge was silently dropped:\n%s", out)
	}
}

// The legitimate case the detector must not fail: a file that can never be a
// violator because it is the subject's DECLARATION home — measure_nav_state.dart
// declares `required String intendedStepCode,`, which the rule's
// `\sintendedStepCode:` alternative can never match, and that entry is a role
// declaration rather than drift. Declared in place with the same discipline as
// `# glob-dead:` — a reviewed comment on the line above, never an allowlist file.
func TestDeclaredExceptDeclarationIsExempt(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: declared-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths:
        # except-declaration: the declaration home; it can never be a violator
        - 'src/decl.go'
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{
		"src/a.go":    "package a\n",
		"src/decl.go": "package decl\n\nconst Name = \"decl\"\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "INERT-EXCEPT") {
		t.Fatalf("a declared declaration-only exception was reported inert:\n%s", out)
	}
}

// The falsifiability arm, and the reason it is not optional.
//
// The probe has to evaluate the rule with its ExceptPaths CLEARED. Forget that
// and engine.Run filters the file out through Applies before any checker sees
// it, every entry in the corpus reports "the rule does not fire here", and the
// arm above passes for the wrong reason on a detector that gates everything.
// This case pins the other direction: an entry the rule genuinely fires on is
// load-bearing and must stay silent.
func TestLoadBearingExceptPathIsNotInert(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: live-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      paths: ['src/excused.go']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{
		"src/a.go":       "package a\n",
		"src/excused.go": "package excused\n\nconst X = \"BANNED\"\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "INERT-EXCEPT") {
		t.Fatalf("an except.paths entry the rule really fires on was reported inert:\n%s", out)
	}
}

// The two verdicts answer different questions and must not collapse into one:
// a MISSING path is drift in the path, an INERT one is drift in the claim. A
// reader sent to the wrong cure learns nothing, so #12178's arm keeps its own
// message.
func TestMissingExceptPathStaysDeadNotInert(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: gone-except
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
		t.Fatalf("a missing except.paths entry lost its DEAD-EXCEPT-GLOB verdict:\n%s", out)
	}
	if strings.Contains(out, "INERT-EXCEPT") {
		t.Fatalf("a missing path was reported as inert rather than dead:\n%s", out)
	}
}
