package main

import (
	"strings"
	"testing"
)

// #12494. The census gates a rule that cannot fail, and a rule keyed on a
// SYMBOL that no longer exists is exactly that — yet none of the existing arms
// can see one. The scope glob still matches thousands of files, so EMPTY-SCOPE
// and EMPTY-GLOB stay silent; a confinement ban is not an existence obligation,
// so the class-2 witness probes never run on it; and the fixtures still
// discriminate, because each fixture carries its own copy of the symbol.
//
// These arms probe the two name-anchored go/* types the engine's own
// rules.AnchorProbe was never wired into: go/call-confined-to-func-name
// (params.symbol) and go/guard-precedes-call (params.sink, params.funcs).

// DEAD-SYMBOL is #12494's own shape: the confined symbol matches no call
// anywhere in scope, so nothing anyone plants can trip the confinement.
func TestDeadSymbolGatesCallConfined(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-symbol
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: 'ReadProjectDisplaySettingsSnapshot\b'
      allowed_func: '^snapshotOnce$'
    tags: [always]
`, map[string]string{
		// The canonical reader moved on; the pinned name survives nowhere.
		"src/a.go": "package a\n\nfunc snapshotOnce() {\n\tprojectfacts.Load()\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-SYMBOL") {
		t.Fatalf("a confined symbol with no call site must gate DEAD-SYMBOL:\n%s", out)
	}
}

// The control that makes the probe mean something. A CORRECTLY CONFINED rule is
// green on the live tree for the opposite reason — the symbol is live and every
// call sits in the allowed func — so a probe that read "the rule passes" as
// "the rule is vacuous" would condemn every healthy confinement in the corpus.
// Only defeating allowed_func separates the two.
func TestConfinedSymbolInAllowedFuncIsNotDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: live-confined
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: 'ReadProjectDisplaySettingsSnapshot\b'
      allowed_func: '^snapshotOnce$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc snapshotOnce() {\n\tReadProjectDisplaySettingsSnapshot()\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-SYMBOL") {
		t.Fatalf("a live symbol confined to its allowed func must not be DEAD-SYMBOL:\n%s", out)
	}
}

// The fixture-carries-the-subject shape, reproduced. The symbol exists in the
// tree, but ONLY inside the rule's own fixture — which is why every fixture arm
// reads green while the live invariant is unguarded.
func TestSymbolSurvivingOnlyInFixturesIsDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: fixture-only-symbol
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: 'ReadProjectDisplaySettingsSnapshot\b'
      allowed_func: '^snapshotOnce$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc snapshotOnce() {\n\tprojectfacts.Load()\n}\n",
	})
	// The last place the subject still exists.
	writeFixture(t, root, "fixture-only-symbol", "pass-1", map[string]string{
		"src/a.go": "package a\n\nfunc snapshotOnce() {\n\tReadProjectDisplaySettingsSnapshot()\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-SYMBOL") {
		t.Fatalf("a symbol living only in the rule's own fixture must gate DEAD-SYMBOL:\n%s", out)
	}
}

// DEAD-SINK: go/guard-precedes-call obliges nothing once the sink it guards is
// gone. No call enters the obligation, so the guard is never asked for.
func TestDeadSinkGatesGuardPrecedes(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-sink
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^authorize$'
      sink: '^deleteEverything$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc handle() {\n\tauthorize()\n\tsave()\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-SINK") {
		t.Fatalf("a guard-precedes rule whose sink matches nothing must gate DEAD-SINK:\n%s", out)
	}
}

// DEAD-FUNCS: the sink is live, but the funcs filter admits no function, so the
// obligation is never entered. Same vacuity one level up — and distinguishable
// from DEAD-SINK only by re-running with the filter dropped.
func TestDeadFuncsFilterGatesGuardPrecedes(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-funcs
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^authorize$'
      sink: '^deleteRow$'
      funcs: '^handleNothing$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc handleDelete() {\n\tauthorize()\n\tdeleteRow()\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "DEAD-FUNCS") {
		t.Fatalf("a live sink behind a funcs filter matching nothing must gate DEAD-FUNCS:\n%s", out)
	}
}

// An admitted function that happens to make NO CALLS is not a dead anchor. The
// first cut decided the funcs filter by a call-count differential — zero
// findings with the filter, some without — and a matched function with an empty
// body produces exactly that reading while the anchor is perfectly live. Whether
// the filter admits anything is a NAME question, which is what the engine's own
// funcAnchor asks for call-order-in-func. Deciding it by call counts repeats the
// condemn-the-obedient inversion this file's header records for the symbol
// anchors, one arm over.
func TestFuncsAnchorMatchingACalllessFuncIsNotDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: callless-admitted
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^authorize$'
      sink: '^deleteRow$'
      funcs: '^handleQuiet$'
    tags: [always]
`, map[string]string{
		// handleQuiet MATCHES funcs and makes no calls; other() makes calls but is
		// not admitted. A call-count differential reads that as a dead filter.
		"src/a.go": "package a\n\nfunc handleQuiet() {\n}\n\nfunc other() {\n\tauthorize()\n\tdeleteRow()\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-FUNCS") {
		t.Fatalf("a funcs anchor matching a call-less function is live, not dead:\n%s", out)
	}
}

// The standing control against over-firing. A DEAD GUARD is not vacuity — it
// makes the rule maximally STRICT, flagging every sink call as unguarded. The
// probe must never report it, or it manufactures findings against rules that
// are enforcing harder than their author intended.
func TestDeadGuardIsNotVacuous(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: dead-guard-live-sink
    type: go/guard-precedes-call
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      guard: '^neverCalled$'
      sink: '^deleteRow$'
    tags: [always]
`, map[string]string{
		// The sink is UNQUALIFIED and undeclared, so it clears the decidability
		// guard and this arm still isolates its own claim: what silences the rule
		// here is the live call site, not the qualification or declaration test.
		"src/a.go": "package a\n\nfunc handle() {\n\tdeleteRow()\n}\n",
	})

	// Asserted against EVERY dead-anchor code, not just the two this file emits
	// today: the claim is "the probe reports nothing here", and naming only the
	// current codes would let a future DEAD-GUARD arm land green through the
	// gap. The corpus declares no exclude, no except and no pair-consistency
	// rule, so no other DEAD- code can appear.
	_, out := census(t, root)
	if strings.Contains(out, "DEAD-") {
		t.Fatalf("a dead guard with a live sink is over-strict, not vacuous:\n%s", out)
	}
}

// The three arms below are the false positives the first cut of this probe
// produced against the real 1873-rule corpus. Every one of them names a symbol
// that is STILL DECLARED AND REACHABLE, so a new call site would trip the rule
// tomorrow: zero call sites today is not "cannot fail" for a BAN. Measured on
// origin/develop at 98d8b1b4 — gemini-generate-content-chokepoint,
// go-one-job-dispatch-body, pipeline-events-read-is-collapsed,
// single-canonical-role-gate and primary-action-files-do-not-reach-per-section-
// loads-indirectly. #12494 asked for a symbol with "no declaration AND no call
// site"; the first cut implemented only the second half.

// A QUALIFIED symbol names something from another package — stdlib, a module
// dependency, or a sibling package. Its declaration was never in this tree, so
// "no declaration here" proves nothing about whether it can be called, and the
// census has no type information to resolve it with. It must not decide.
//
// go-one-job-dispatch-body is the measured case: it pins json.Marshal inside
// one file that has since moved to proto.Marshal. The ban is still live —
// reintroducing json.Marshal there is exactly what it exists to catch.
func TestQualifiedSymbolIsNeverDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: qualified-symbol
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: 'json\.Marshal'
      allowed_func: '^encodePayload$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc encodePayload(m M) ([]byte, error) {\n\treturn proto.Marshal(m)\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-") {
		t.Fatalf("a package-qualified symbol resolves outside this tree and is never decidable here:\n%s", out)
	}
}

// An unqualified symbol that is DECLARED in the tree is live even with no call
// site in scope: the function exists, so a new caller can appear. Only a symbol
// with no declaration anywhere — the #12494 shape, where the func was deleted —
// is unfallible. single-canonical-role-gate is the measured case: FindRole is
// declared in the addrole package and merely has no caller outside rolegate,
// which is the rule being OBEYED.
func TestDeclaredSymbolWithNoCallSiteIsNotDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: declared-uncalled
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/routes/**/*.go']
    params:
      symbol: '^FindRole$'
      allowed_func: '^Validate$'
    tags: [always]
`, map[string]string{
		// Declared outside the rule's scope, called by nobody: compliance.
		"src/addrole/addrole.go": "package addrole\n\nfunc FindRole(pageType string) bool {\n\treturn true\n}\n",
		"src/routes/handler.go":  "package routes\n\nfunc Handle() {\n\tbuildOptions()\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-") {
		t.Fatalf("a declared symbol with no call site is obeyed, not dead:\n%s", out)
	}
}

// A TOTAL BAN — allowed_func matching no Go identifier — is satisfied by having
// no call sites at all. Zero hits is its compliant state, so the confinement
// framing the DEAD-SYMBOL verdict uses does not even apply.
// primary-action-files-do-not-reach-per-section-loads-indirectly is the
// measured case, and its own comment says so: "^$ matches no Go identifier, so
// this is a total ban rather than a confinement." It carves the legitimate
// callers out through scope.exclude and bans the rest.
func TestTotalBanWithNoCallSitesIsNotDead(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: total-ban
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/routes/**/*.go']
      exclude: ['src/routes/owner.go']
    params:
      symbol: '^sectionsComplete$'
      allowed_func: '^$'
    tags: [always]
`, map[string]string{
		// The one legitimate home, carved out by scope.exclude.
		"src/routes/owner.go": "package routes\n\nfunc sectionsComplete() bool {\n\treturn true\n}\n\nfunc use() {\n\tsectionsComplete()\n}\n",
		// Everything else obeys the ban.
		"src/routes/other.go": "package routes\n\nfunc Handle() {\n\tsectionsCompleteFrom()\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-") {
		t.Fatalf("a total ban with no call sites is fully satisfied, not vacuous:\n%s", out)
	}
}

// A rule this arm CANNOT decide must be counted out loud, never folded into the
// class-2 zero. Measured on origin/develop at 98d8b1b4, 30 of the 41
// name-anchored Go rules carry a package selector, so a bare "class 2 — 0"
// reports 41 rules cleared when 11 were examined. The census already binds
// itself to this for set-relation and except.paths, in its own words: a hole
// that goes unnamed reads as coverage, which is the defect the census exists to
// catch, one level up.
func TestUndecidedSymbolAnchorsAreDisclosed(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: undecidable-qualified
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    params:
      symbol: 'json\.Marshal'
      allowed_func: '^encodePayload$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc encodePayload(m M) ([]byte, error) {\n\treturn proto.Marshal(m)\n}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "symbol-anchored rules NOT decided here") {
		t.Fatalf("an undecidable symbol anchor must be disclosed, not folded into the class-2 zero:\n%s", out)
	}
}

// A SUPPRESSED finding still proves the symbol exists. The rule is green on
// this tree — every call site carries a formwork:allow marker — but a new
// unmarked call would trip it tomorrow, so the anchor is live. Reading only
// unsuppressed findings would call a fully-marked live symbol dead and retire a
// working gate; this is the same reading exceptExcuses uses.
//
// except.marker is what arms the marker channel: without it the engine never
// consults an inline formwork:allow at all, the comment below is inert, and
// this arm silently stops testing suppression (it did, until #12494 caught it).
func TestAllowlistSuppressedCallSiteIsStillLive(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: suppressed-but-live
    type: go/call-confined-to-func-name
    severity: error
    scope:
      include: ['src/**/*.go']
    except:
      marker: true
    params:
      symbol: 'ConsumeTakeoff\b'
      allowed_func: '^chokepoint$'
    tags: [always]
`, map[string]string{
		"src/a.go": "package a\n\nfunc rogue() {\n\tConsumeTakeoff() // formwork:allow suppressed-but-live legacy call site\n}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "DEAD-SYMBOL") {
		t.Fatalf("a symbol whose only call site is marker-suppressed is still live:\n%s", out)
	}
}
