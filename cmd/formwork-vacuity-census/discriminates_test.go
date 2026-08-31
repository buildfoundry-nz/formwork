package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The corrections below answer the SECOND half of #12178: a class promoted to
// gating is only worth gating if the detector behind it discriminates. Three of
// the promoted detectors do not, and one of the instrument defects was measured
// with a probe that reads a rule type's documented semantics as a defect.
// ---------------------------------------------------------------------------

// A pair-consistency rule whose `trigger` matches nothing in scope can never
// oblige a companion: no file will ever enter the obligation, so the rule is
// green whatever the tree does. That is the sound pair-consistency vacuity —
// the same shape as the engine's own AnchorProbe one rule type over.
func TestPairConsistencyDeadTriggerGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-trigger
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      trigger: 'platformops\.RecordControlRenamedAwayLongAgo'
      requires: 'audit_events'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc A() { audit_events() }\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-TRIGGER") {
		t.Fatalf("no DEAD-TRIGGER verdict in output:\n%s", out)
	}
}

// A `disjoint` set-relation has no REQUIRED side. `params.b` is the FORBIDDEN
// side: the invariant is A ∩ B = ∅, so emptying B satisfies it by definition,
// and every disjoint rule in every corpus reads as "nothing on the b side was
// load-bearing". The rule below fires on its real defect — the shared element
// is right there — so the census must stay quiet about it.
//
// Measured on this corpus the blank-the-b-side probe reported 23 set-relation
// rules and 6 of them were exactly this: no-shadow-repo-of-base-endpoint,
// no-fe-section-policy, deep-link-tables-are-disjoint,
// slots-uc-mask-has-no-page-metric, promotion-jobtype-not-relitigated-as-a-literal,
// detail-catalog-no-bespoke-keymap. A disjoint relation is falsified by ADDING
// an element to both sides, never by removing one.
func TestDisjointRelationIsNotProbedByBlankingTheForbiddenSide(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: live-disjoint
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      a: { files: ['src/left/**'], pattern: 'tag:([a-z]+)', group: 1 }
      b: { files: ['src/right/**'], pattern: 'tag:([a-z]+)', group: 1 }
      relation: disjoint
    tags: [always]
`, map[string]string{
		"src/left/a.go":  "package a\n// tag:alpha\n",
		"src/right/b.go": "package b\n// tag:beta\n",
	})

	if _, out := census(t, root); strings.Contains(out, "INERT-REQUIRED-SIDE") {
		t.Fatalf("a disjoint relation was reported vacuous for having an emptiable forbidden side:\n%s", out)
	}
}

// The other half of the same category error. When the two sides draw from the
// SAME files, blanking "the b side" blanks the a side with it: A empties
// alongside B and `A ⊆ B` holds trivially. The probe is not reading the rule,
// it is reading the fact that one glob appears twice. Five of the 23 were this
// — queryobs-acquire-claim-has-its-tracer, partition-parents-have-ci-default,
// admin-jobs-have-machine-twin, no-write-only-takeoffqs-tables,
// cladding-family-set-lists-every-declared-code — and two more were the same
// shape spelled as a containing glob. The rule below fires on its real defect:
// add a write with no read and it goes red.
func TestSameGlobRelationIsNotProbedByBlankingBothSidesAtOnce(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: live-same-glob
    type: set-relation
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      a: { files: ['src/**/*.go'], pattern: 'write:([a-z]+)', group: 1 }
      b: { files: ['src/**/*.go'], pattern: 'read:([a-z]+)', group: 1 }
      relation: subset
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n// write:alpha\n// read:alpha\n",
	})

	if _, out := census(t, root); strings.Contains(out, "INERT-REQUIRED-SIDE") {
		t.Fatalf("a same-glob relation was reported vacuous for emptying both sides at once:\n%s", out)
	}
}

// The converse, and the reason COMPANION-COUNT-BLIND cannot be the probe.
// `where: same-file` is pair-consistency's DEFAULT and its documented meaning:
// a trigger match obliges a `requires` match within the same unit — presence,
// not pairing. So a second trigger beside an existing companion is compliant by
// definition, and a probe that duplicates a trigger and calls the resulting
// green a defect condemns the rule type rather than the rule. Measured on this
// corpus it flagged 93 of 106 pair-consistency rules.
func TestHealthySameFilePairConsistencyIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: live-pair
    type: pair-consistency
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      trigger: 'RecordControl\('
      requires: 'audit_events'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc A() {\n\tRecordControl()\n\t// audit_events\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "COMPANION-COUNT-BLIND") {
		t.Fatalf("a healthy same-file pair-consistency rule was condemned for its own "+
			"documented presence semantics:\n%s", out)
	}
	if strings.Contains(out, "DEAD-TRIGGER") {
		t.Fatalf("a live trigger was reported dead:\n%s", out)
	}
}

// The 512 "absence-only fixtures" are an artefact of the detector, not a
// property of the fixtures. It reports a rule whose every fire finding lacks a
// Path — but required-pattern mode:exists, pattern-count and set-relation all
// emit their verdict from Finalize, where the engine has no file to attribute
// it to (engine.toFinding's fallbackPath is ""), so a Path is not something any
// fixture of those types can produce. Demanding one is demanding the
// impossible, and the whole population is exactly those types.
func TestExistenceRuleWithARealFireFixtureIsNotAbsenceOnly(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: exists-rule
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { mode: exists, pattern: 'WantedToken' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n\nconst X = \"WantedToken\"\n"})
	// fire-1 carries an in-scope file whose token is WRONG — a near-miss, not a
	// deletion. pass-1 carries the right one.
	writeFixture(t, root, "exists-rule", "fire-1", map[string]string{
		"src/a.go": "package a\n\nconst X = \"WantedTokenn\"\n",
	})
	writeFixture(t, root, "exists-rule", "pass-1", map[string]string{
		"src/a.go": "package a\n\nconst X = \"WantedToken\"\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "ABSENCE-ONLY") {
		t.Fatalf("a near-miss fire fixture was still reported absence-only — the detector is "+
			"measuring the rule TYPE, not the fixture:\n%s", out)
	}
}

// What the absence-only number was reaching for, expressed so it discriminates:
// a fire fixture with NO in-scope file at all fires because the scope is empty,
// not because the evidence is wrong. pattern-count op:at-least fires on an empty
// tree (total 0 < n), so such a fixture demonstrates nothing about the pattern.
func TestFireFixtureWithNoInScopeFileGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: empty-scope-fire
    type: pattern-count
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { pattern: 'MARKER', op: at-least, n: 1 }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n\n// MARKER\n"})
	writeFixture(t, root, "empty-scope-fire", "fire-1", map[string]string{
		"docs/readme.md": "nothing in scope here\n",
	})
	writeFixture(t, root, "empty-scope-fire", "pass-1", map[string]string{
		"src/a.go": "package a\n\n// MARKER\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "EMPTY-SCOPE-FIRE-FIXTURE") {
		t.Fatalf("a fire fixture with no in-scope file was not gated:\n%s", out)
	}
}

// DIFFUSE-EVIDENCE is restricted to len(ws) <= 3, which excludes exactly the
// rules whose evidence is most redundant. A rule with four diffuse witnesses is
// no less untrippable than one with three.
func TestDiffuseEvidenceReachesBeyondThreeWitnesses(t *testing.T) {
	var body strings.Builder
	body.WriteString("package a\n")
	for i := 0; i < 60; i++ {
		body.WriteString("// SPREAD\n")
	}
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		files["src/"+n+".go"] = body.String()
	}
	root := writeCorpus(t, `rules:
  - id: spread-rule
    type: required-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
    params: { mode: exists, pattern: 'SPREAD' }
    tags: [always]
`, files)

	_, out := census(t, root)
	if !strings.Contains(out, "DIFFUSE-EVIDENCE") {
		t.Fatalf("five diffuse witnesses were out of the arm's reach:\n%s", out)
	}
}

// The census excuses every heavy rule (type:command, git-diff) from carrying a
// fixture on the grounds that "their fire coverage lives in
// api-factory/internal/lockdowntests/". That excuse is asserted, never checked.
// A heavy rule no lockdown synth names has nothing, anywhere, demonstrating it
// can fail.
func TestHeavyRuleWithNoFixtureAndNoSynthGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: unwitnessed-command
    type: command
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      cmd: ['true']
      expect: { exit: 0 }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})

	_, out := census(t, root)
	if !strings.Contains(out, "NO-FIRE-WITNESS") {
		t.Fatalf("a heavy rule with neither a fixture nor a lockdown synth was not gated:\n%s", out)
	}
}

// The other half of the same check: a heavy rule whose lockdown synth is a
// comment-only marker inventory with no test function in it is not witnessed
// either. api-factory/internal/lockdowntests/synth_form_coverage_markers_test.go
// is twelve lines of `// FORM(...)` markers and no code.
func TestHeavyRuleWitnessedOnlyByACommentFileGates(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: marker-only-command
    type: command
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      cmd: ['true']
      expect: { exit: 0 }
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n",
		"api-factory/internal/lockdowntests/marker_only_test.go": "//go:build lockdown\n\npackage lockdowntests\n\n" +
			"// marker-only-command: class covered elsewhere\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "NO-FIRE-WITNESS") {
		t.Fatalf("a heavy rule whose only synth has no test function was not gated:\n%s", out)
	}
}

// And the true negative: a heavy rule with a real lockdown synth naming it is
// witnessed, and must stay quiet.
func TestHeavyRuleWithARealSynthIsQuiet(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: witnessed-command
    type: command
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      cmd: ['true']
      expect: { exit: 0 }
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n",
		"api-factory/internal/lockdowntests/witnessed_command_synthetic_test.go": "//go:build lockdown\n\n" +
			"package lockdowntests\n\nimport \"testing\"\n\n" +
			"func TestWitnessedCommandFires(t *testing.T) {\n\t_ = \"witnessed-command\"\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "NO-FIRE-WITNESS") {
		t.Fatalf("a heavy rule with a real lockdown synth was reported unwitnessed:\n%s", out)
	}
}

// The exclude arm's OTHER half, and the reason "matches nothing" cannot be the
// whole test. A wildcard exclude is a CLASS GUARD: `**/node_modules/**`,
// `**/*.pbjson.dart`, `**/.dart_tool/**`, `.agent-work/**` name a class of path
// that can appear at any time — a build output, a generated proto variant, an
// agent scratch tree. Matching nothing today is the guard holding, not the
// guard rotting, and deleting it is a regression. Measured against this corpus,
// 232 of the 263 dead exclude/except globs are exactly that shape and not one
// of them is a coverage loss; the 31 that remain all name a literal path, and
// every one is a scripts/check-*.sh gate formwork replaced and deleted, or a
// file that moved.
func TestWildcardClassGuardExcludeIsNotADeadGlob(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: guarded
    type: forbidden-pattern
    severity: error
    scope:
      include: ['src/**/*.go']
      exclude: ['**/node_modules/**', '**/*.pb.go', '.agent-work/**']
    params: { pattern: 'BANNED' }
    tags: [always]
`, map[string]string{"src/a.go": "package a\n"})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-EXCLUDE-GLOB") {
		t.Fatalf("a wildcard class guard was condemned for not having fired yet:\n%s", out)
	}
}
