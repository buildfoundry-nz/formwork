// Corpus-independent calibration for the class-2 verdict probes (#15917).
//
// WHY THIS FILE EXISTS, and it is not tidiness. mutation-proof materialises a
// scratch corpus PRUNED to the rule under test alone (rewriteRuleFileToSingleID
// in scripts/dev/mutation-proof), and the survivor is
// formwork-rules-not-vacuous itself — a type: command rule. Class-2 verdicts are
// witness probes over CONTENT rules, so in that scratch there is nothing for
// them to classify and the verdict is over an empty population whatever the
// logic says. Demonstrated, not argued: a spec mutating classify.go so
// DETECTOR-SATISFIED fires for EVERY witness-bearing rule was answered by
// mutation-proof with "did not reject the mutation — it stayed green on wrong
// evidence inside its own scope".
//
// So the one arm of this census that could be proven was the fixture-registry
// arm, which reads the TREE rather than the pruned rule corpus. The constraint
// SELECTED the arm that got proven, and it is the arm whose loss costs least.
//
// arm-shape-ratchets hit the identical wall and its spec notes record the cure:
// a corpus-INDEPENDENT calibration that runs BEFORE the corpus scan, so a
// mutation to the verdict logic is caught by calibration rather than by a
// corpus the scratch does not have. This census already had that mechanism —
// selfTest() in calibrate.go — and it was scoped to the scope MATCHER alone,
// the arm that needed it least. Same pattern one level down.
//
// This block calibrates the two load-bearing class-2 probes on inputs whose
// answers are known, so they are falsifiable without any corpus at all.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// class2SelfTestRule is the throwaway corpus the calibration judges against.
// It is written to a temp dir and loaded through config.Load rather than built
// with config.New, because config.New leaves the rule's factory unset and
// Rule.Fresh() then fails — which satisfies() reports as "does not satisfy".
// A calibration built that way would answer "no verdict" to every case and
// pass while measuring nothing, which is the defect it exists to catch.
const class2SelfTestRule = `rules:
  - id: class2-selftest
    type: required-pattern
    severity: error
    scope:
      include: ['**/*.go']
    params:
      pattern: govulncheck
      mode: every-file
    tags: [always]
`

// class2SelfTest proves the class-2 verdict probes still classify known inputs
// correctly. Corpus-independent: it builds its own throwaway corpus, so it
// calibrates inside a pruned proof scratch exactly as it does on the real tree.
//
// Each case is one probe's DISCRIMINATION, never just its firing: every arm
// that must fire is paired with one that must not. A probe that fired on
// everything would pass a fire-only calibration and report the whole corpus
// vacuous, which is the same failure as one that fires on nothing.
func class2SelfTest() error {
	dir, err := os.MkdirTemp("", "vacuity-class2-")
	if err != nil {
		return fmt.Errorf("class-2 calibration: temp corpus: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".formwork", "rules"), 0o755); err != nil {
		return fmt.Errorf("class-2 calibration: temp corpus: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".formwork", "formwork.yaml"),
		[]byte("version: 1\nlanes:\n  always: { tags: [always] }\n"), 0o644); err != nil {
		return fmt.Errorf("class-2 calibration: temp corpus: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".formwork", "rules", "selftest.yaml"),
		[]byte(class2SelfTestRule), 0o644); err != nil {
		return fmt.Errorf("class-2 calibration: temp corpus: %w", err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Errorf("class-2 calibration: loading temp corpus: %w", err)
	}
	var r *config.Rule
	for _, c := range cfg.Rules {
		if c.ID == "class2-selftest" {
			r = c
		}
	}
	if r == nil {
		return fmt.Errorf("class-2 calibration: the temp corpus did not yield its own rule")
	}

	commentOnly := scan.NewMemFile("api-factory/a.go", []byte("// govulncheck runs in CI\npackage a\n"))
	codeBacked := scan.NewMemFile("api-factory/b.go", []byte("// nothing to see here\nvar x = govulncheck\n"))
	detector := scan.NewMemFile("scripts/dev/gate.go", []byte("var x = govulncheck\n"))
	product := scan.NewMemFile("api-factory/p.go", []byte("package p\n"))

	cases := []struct {
		name    string
		ws      []*scan.File
		inScope []*scan.File
		code    string
		want    bool
		why     string
	}{
		{
			name: "comment-only witness", ws: []*scan.File{commentOnly}, inScope: []*scan.File{commentOnly},
			code: "COMMENT-SUFFICIENT", want: true,
			why: "the token lives only in a comment, so every line of the subject could be deleted and the prose would hold the gate green",
		},
		{
			name: "code-backed witness", ws: []*scan.File{codeBacked}, inScope: []*scan.File{codeBacked},
			code: "COMMENT-SUFFICIENT", want: false,
			why: "the comment plane alone does not satisfy this rule, so the evidence is the code",
		},
		{
			name: "gate witness for a rule reaching product code", ws: []*scan.File{detector},
			inScope: []*scan.File{detector, product},
			code:    "DETECTOR-SATISFIED", want: true,
			why: "the only witness is a gate source while the scope reaches product code — the product could be deleted whole and the detector's own mention would keep it green",
		},
		{
			name: "gate witness for a gate-about-a-gate rule", ws: []*scan.File{detector},
			inScope: []*scan.File{detector},
			code:    "DETECTOR-SATISFIED", want: false,
			why: "the rule's whole subject IS the gate tree, so a gate witness is the correct one and condemning it would condemn every gate-about-a-gate rule in the corpus",
		},
	}
	for _, c := range cases {
		got := false
		for _, v := range class2Verdicts(r, dir, c.ws, c.inScope) {
			if v.code == c.code {
				got = true
			}
		}
		if got != c.want {
			return fmt.Errorf("class-2 calibration %q: %s %s, want %s — %s. The probe no longer "+
				"discriminates, so every class-2 zero this census prints would be unfalsifiable",
				c.name, c.code, verdictWord(got), verdictWord(c.want), c.why)
		}
	}
	return nil
}

// verdictWord renders a fired/absent verdict for the calibration's error text.
func verdictWord(fired bool) string {
	if fired {
		return "fired"
	}
	return "did not fire"
}
