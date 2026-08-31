package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #15103 — the shape the census could not see.
//
// tracked-sh-shrink-only was UNPROVABLE by construction and this census said
// "OK: every rule can fail". Its scope was two globs: `**/*.sh`, which matched
// nothing and was waived from the EMPTY-GLOB arm by a `# glob-dead:`
// annotation, and its own detector source. So the only in-scope file was the
// program named in params.cmd, and every mutation of that file breaks the
// invocation — the rule exits 1 on "no such file", the right exit number for
// the wrong reason, which is the false green a proof exists to refuse.
//
// Two exemptions met to hide it. The dead glob was silent by declaration, and
// detectorWitnesses returns nil when nothing in scope lives outside scripts/ or
// tools/ — "the rule's whole subject is the gate tree; a gate witness is
// correct", which is right for the ~145 rules that really are about gate code
// and wrong for exactly this one. The only instrument that ever objected was
// the mutation-proof runner, and that selects on the diff, so a rule sitting in
// this shape untouched is never examined at all.
//
// The arm is the corpus-wide form of that objection, narrowed to the shape
// where silence is indefensible: when every file a rule's scope reaches is
// machinery the rule's own command runs, the mutation spec is the only evidence
// left that the rule asserts anything, so its absence gates.

var _ = selfProofVerdicts

// writeSpec plants a mutation spec for a rule. Contents are irrelevant here —
// the census asks whether the rule carries a proof, and the mutation-proof
// runner is what asks whether the proof works.
func writeSpec(t *testing.T, root, ruleID string) {
	t.Helper()
	p := filepath.Join(root, ".formwork", "mutations", ruleID+".yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "rule: " + ruleID + "\ntarget: scripts/dev/check-thing.go\nmutation:\n  kind: replace-literal\n  find: 'A'\n  replace: 'B'\nexpect: rule-fails\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// selfScopedCorpus is the historical tracked-sh-shrink-only shape: a dead glob
// carrying its documented opt-out, plus the detector's own source, which is
// also the program params.cmd runs and the rule's declared origin.
func selfScopedCorpus(t *testing.T) string {
	t.Helper()
	return writeCorpus(t, `rules:
  - id: self-scoped-census
    type: command
    severity: error
    scope:
      include:
        # glob-dead: the class is closed by ban, not by absence
        - "**/*.sh"
        - "scripts/dev/check-thing.go"
    params:
      cmd: [go, run, scripts/dev/check-thing.go, .]
      expect: { exit: 0 }
    origin: scripts/dev/check-thing.go
    tags: [always]
`, map[string]string{
		"scripts/dev/check-thing.go": "package main\n\nfunc main() {}\n",
		"src/product.go":             "package src\n",
	})
}

// The defect itself. Every in-scope file is the detector params.cmd runs, no
// spec exists, and the census must say so rather than reporting the rule as one
// that can fail.
func TestSelfScopedCommandRuleWithoutASpecGates(t *testing.T) {
	root := selfScopedCorpus(t)

	code, out := census(t, root)
	if !strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("a rule whose whole scope is its own detector, with no mutation spec, was not reported:\n%s", out)
	}
	if !strings.Contains(out, "self-scoped-census") || !strings.Contains(out, "scripts/dev/check-thing.go") {
		t.Fatalf("the verdict does not name the rule and the machinery it is trapped inside:\n%s", out)
	}
	if code == 0 {
		t.Fatalf("census exited 0 on an unprovable rule:\n%s", out)
	}
}

// The discriminator that keeps the arm from crying wolf on the ~145 rules whose
// subject legitimately IS gate code. A spec is the evidence the rule can be
// falsified; carrying one ends the question here, and whether the proof
// actually bites is the mutation-proof runner's verdict, not this one's.
func TestSelfScopedCommandRuleWithASpecIsSilent(t *testing.T) {
	root := selfScopedCorpus(t)
	writeSpec(t, root, "self-scoped-census")

	_, out := census(t, root)
	if strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("a rule carrying a mutation spec was still reported unprovable:\n%s", out)
	}
}

// The other half of the discriminator, and the one the arm would be worthless
// without: formwork-meta-tool-houses-no-new-check-shell is gate-tree-scoped in
// exactly the way detectorWitnesses waives, but one in-scope file is NOT its own
// machinery — which is what its passing proof mutates. Scope reaching one file
// the command does not run is enough; the rule is provable and must stay quiet.
func TestCommandRuleReachingBeyondItsMachineryIsSilent(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: gate-tree-scoped
    type: command
    severity: error
    scope:
      include:
        - "scripts/dev/check-thing.go"
        - "scripts/baselines/roster.txt"
    params:
      cmd: [go, run, scripts/dev/check-thing.go, .]
      expect: { exit: 0 }
    origin: scripts/dev/check-thing.go
    tags: [always]
`, map[string]string{
		"scripts/dev/check-thing.go":   "package main\n\nfunc main() {}\n",
		"scripts/baselines/roster.txt": "check-diff.sh\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("a command rule whose scope reaches a file its own command does not run was reported unprovable:\n%s", out)
	}
}

// A declarative rule over its own origin is a different animal and must not be
// swept up. Nothing executes the file — the engine reads it — so a content
// mutation of that very file is a valid proof, which is why the shape is
// restricted to type:command.
func TestDeclarativeRuleOverItsOwnOriginIsSilent(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: declarative-over-origin
    type: required-pattern
    severity: error
    scope:
      include: ["scripts/dev/check-thing.go"]
    params: { pattern: 'REQUIRED', mode: every-file }
    origin: scripts/dev/check-thing.go
    tags: [always]
`, map[string]string{
		"scripts/dev/check-thing.go": "package main\n\n// REQUIRED\nfunc main() {}\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("a declarative rule over its own origin was reported unprovable:\n%s", out)
	}
}

// `go -C <module> run .` names a directory, not a file list, and the module it
// compiles is machinery just the same. A rule scoped at that module and nothing
// else is the same trap spelled differently.
func TestModuleFormCommandIsRecognisedAsMachinery(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: module-form-detector
    type: command
    severity: error
    scope:
      include: ["scripts/dev/thing/**"]
    params:
      cmd: [go, -C, scripts/dev/thing, run, ., --root, ../../..]
      expect: { exit: 0 }
    origin: scripts/dev/thing/main.go
    tags: [always]
`, map[string]string{
		"scripts/dev/thing/go.mod":  "module thing\n\ngo 1.24\n",
		"scripts/dev/thing/main.go": "package main\n\nfunc main() {}\n",
		"src/product.go":            "package src\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("the `go -C <module> run .` form was not recognised as the rule's own machinery:\n%s", out)
	}
}

// The shared-library shape, and the sharpest of the seven calibration cases.
//
// count-relation-arm-is-anchored and exists-rule-cure-not-universal each scope
// their own detector module PLUS tools/formworkcensus/**, a library the
// detector LINKS rather than names on its command line. Every file in scope is
// still machinery — mutating the shared library is mutating the detector, one
// level down — but an arm reading only argv paths sees the library as an
// outside witness and stays silent. The link is what has to be resolved.
func TestLinkedGateLibraryIsMachinery(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: links-a-gate-library
    type: command
    severity: error
    scope:
      include:
        - "tools/thing-census/**"
        - "tools/gatelib/**"
    params:
      cmd: [go, run, -C, tools/thing-census, ., ../..]
      expect: { exit: 0 }
    origin: tools/thing-census/main.go
    tags: [always]
`, map[string]string{
		"tools/gatelib/go.mod":       "module example.com/gatelib\n\ngo 1.24\n",
		"tools/gatelib/lib.go":       "package gatelib\n\n// Tag names this library in a finding.\nfunc Tag() string { return \"gatelib\" }\n",
		"tools/thing-census/go.mod":  "module example.com/thingcensus\n\ngo 1.24\n\nrequire example.com/gatelib v0.0.0\n\nreplace example.com/gatelib => ../gatelib\n",
		"tools/thing-census/main.go": "package main\n\nimport \"example.com/gatelib\"\n\nfunc main() { _ = gatelib.Tag() }\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("a library the detector links, not names, was read as an outside witness:\n%s", out)
	}
	if !strings.Contains(out, "tools/gatelib/lib.go") {
		t.Fatalf("the verdict does not name the linked library among the machinery:\n%s", out)
	}
}

// The other side of the link check, and the reason it is bounded to the gate
// tree. A detector that imports a product package does not thereby make that
// package machinery: the package is the rule's SUBJECT, and mutating it is the
// proof. Only a library under scripts/ or tools/ — the gate fleet's own shared
// code — is machinery one level down.
func TestLinkedProductPackageIsNotMachinery(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: links-product-code
    type: command
    severity: error
    scope:
      include:
        - "tools/thing-census/**"
        - "api-factory/internal/subject/**"
    params:
      cmd: [go, run, -C, tools/thing-census, ., ../..]
      expect: { exit: 0 }
    origin: tools/thing-census/main.go
    tags: [always]
`, map[string]string{
		"api-factory/internal/subject/go.mod":     "module example.com/subject\n\ngo 1.24\n",
		"api-factory/internal/subject/subject.go": "package subject\n\n// Name is the thing the rule asserts about.\nfunc Name() string { return \"subject\" }\n",
		"tools/thing-census/go.mod":               "module example.com/thingcensus\n\ngo 1.24\n\nrequire example.com/subject v0.0.0\n\nreplace example.com/subject => ../../api-factory/internal/subject\n",
		"tools/thing-census/main.go":              "package main\n\nimport \"example.com/subject\"\n\nfunc main() { _ = subject.Name() }\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("a product package the detector merely imports was counted as machinery:\n%s", out)
	}
}

// `go run -C <dir> . <arg>` puts the flag AFTER the run verb, which eight rules
// in the live corpus do. A resolver that stops at the first dash never reaches
// the package and reads the detector's own module as an outside witness — the
// blind spot that hid the two linked-library cases.
func TestFlagAfterRunVerbStillResolvesTheModule(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: flag-after-run
    type: command
    severity: error
    scope:
      include: ["tools/thing-census/**"]
    params:
      cmd: [go, run, -C, tools/thing-census, ., ../..]
      expect: { exit: 0 }
    origin: tools/thing-census/main.go
    tags: [always]
`, map[string]string{
		"tools/thing-census/go.mod":       "module example.com/thingcensus\n\ngo 1.24\n",
		"tools/thing-census/main.go":      "package main\n\nfunc main() {}\n",
		"tools/thing-census/main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	})

	_, out := census(t, root)
	if !strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("the run-flag form hid the detector's own module from the resolver:\n%s", out)
	}
	if !strings.Contains(out, "tools/thing-census/main_test.go") {
		t.Fatalf("the module's own files were not all resolved as machinery:\n%s", out)
	}
}

// The detector's own root argument is not a package. `../..` is how every
// go-run detector here is handed the repo root, and resolving it as a package
// path walks out of the tree entirely.
func TestDetectorRootArgumentIsNotAPackageTarget(t *testing.T) {
	root := writeCorpus(t, `rules:
  - id: root-arg-not-a-package
    type: command
    severity: error
    scope:
      include:
        - "tools/thing-census/**"
        - "api-factory/internal/subject/subject.go"
    params:
      cmd: [go, run, -C, tools/thing-census, ., ../..]
      expect: { exit: 0 }
    origin: tools/thing-census/main.go
    tags: [always]
`, map[string]string{
		"tools/thing-census/go.mod":               "module example.com/thingcensus\n\ngo 1.24\n",
		"tools/thing-census/main.go":              "package main\n\nfunc main() {}\n",
		"api-factory/internal/subject/subject.go": "package subject\n",
	})

	_, out := census(t, root)
	if strings.Contains(out, "SELF-DETECTOR-UNPROVEN") {
		t.Fatalf("the repo-root argument was resolved as a package and swallowed the subject:\n%s", out)
	}
}
