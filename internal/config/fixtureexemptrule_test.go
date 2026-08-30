// fixtureexemptrule_test.go — #336, the governed-content half.
//
// fixtureExemptReason (fixtureexempt_test.go) closes the field at the one
// moment the file is CONFIG: `config.Load` refuses a content-free declaration
// and exits 2. That refusal reaches exactly the rule files this binary is
// pointed at as its own configuration. A rule file can also arrive as governed
// CONTENT — a ported corpus under examples/, a vendored subproject, a
// downstream repository pinning an older binary — and on that surface nothing
// parses the file as config at all. `make check` at this repo's root scans 616
// rule files under examples/palletra-port-full alone and loads none of them.
//
// So the class gets a formwork rule as well, .formwork/rules/
// fixture-exempt-declares-nothing.yaml, and this test is the seam between the
// two halves: the property that keeps them from drifting apart. A gate that
// disagreed with the loader would be #336 again with the operands swapped —
// one surface calling a declaration good and the other calling it bad — so
// the two directions that would BE that disagreement are derived from the
// loader here rather than tabled:
//
//	the loader stores a reason      => the rule must be silent
//	the loader refuses the value    => the rule must fire
//
// The remaining class is the one the loader cannot see: a value that decodes
// to "", which is indistinguishable from an absent key (fixtureexempt.go says
// so) and is reported by fixture-coverage rather than refused. The bytes on
// disk still differ, and only some of them read as a declaration, so that
// class is tabled explicitly below.
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// blankDeclarationRulePath is the governed-content half of #336, read from the
// tree rather than restated, so this test measures the shipped rule.
const blankDeclarationRulePath = ".formwork/rules/fixture-exempt-declares-nothing.yaml"

// formworkRepoRoot walks up to the directory holding go.mod — fail-closed, so
// the test cannot silently read some other tree's rules.
func formworkRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

// declaredRuleFile is a heavy rule carrying decl verbatim, at the corpus shape
// the field is actually written on. decl is spliced in whole so a block scalar
// and its body arrive exactly as a rule author would type them.
func declaredRuleFile(decl string) string {
	return "rules:\n" +
		"  - id: dart-format-clean\n" +
		"    type: command\n" +
		decl +
		"    scope: {include: ['**/*.dart']}\n" +
		"    params: {cmd: [true]}\n"
}

// loaderVerdict is what config.Load makes of decl when the file IS the config.
type loaderVerdict int

const (
	loaderRefused    loaderVerdict = iota // exit 2 — the value declares nothing
	loaderUndeclared                      // decodes to "" — same as an absent key
	loaderDeclared                        // a reason is stored
)

func (v loaderVerdict) String() string {
	switch v {
	case loaderRefused:
		return "refused"
	case loaderUndeclared:
		return "undeclared"
	default:
		return "declared"
	}
}

func askLoader(t *testing.T, decl string) loaderVerdict {
	t.Helper()
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml":  declaredRuleFile(decl),
	})
	cfg, err := config.Load(root)
	if err != nil {
		if !strings.Contains(err.Error(), "fixture_exempt") {
			t.Fatalf("loading %q failed for an unrelated reason: %v", decl, err)
		}
		return loaderRefused
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("loading %q produced %d rules, want 1", decl, len(cfg.Rules))
	}
	if cfg.Rules[0].FixtureExempt == "" {
		return loaderUndeclared
	}
	return loaderDeclared
}

// askRule plants decl in a rule file at the shape a ported corpus has —
// examples/<corpus>/.formwork/rules/<id>.yaml, which the walk reaches because
// `.formwork` prunes only as a direct child of the walk root (#268) — and
// reports whether the shipped rule finds it. The planted file is never loaded
// as configuration by this run: that is the whole surface the rule exists for.
func askRule(t *testing.T, rule *config.Rule, decl string) bool {
	t.Helper()
	root := writeRepo(t, map[string]string{
		"examples/demo-corpus/.formwork/rules/dart-format-clean.yaml": declaredRuleFile(decl),
	})
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(fset.Files) != 1 {
		t.Fatalf("the plant tree walked %d files, want exactly the planted rule file", len(fset.Files))
	}
	fresh, err := rule.Fresh()
	if err != nil {
		t.Fatal(err)
	}
	found, err := engine.Run([]*config.Rule{fresh}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	return len(finding.Unsuppressed(found)) > 0
}

func TestTheBlankDeclarationRuleAgreesWithTheLoader(t *testing.T) {
	root := formworkRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(blankDeclarationRulePath)))
	if err != nil {
		t.Fatalf("%s is the governed-content half of #336 and this tree does not carry it: %v\n"+
			"config.Load refuses a content-free `fixture_exempt` only where the file is read AS\n"+
			"CONFIG. A ported corpus, a vendored subproject and a downstream repo pinning an\n"+
			"older binary read the same file as governed content and nothing parses it there.",
			blankDeclarationRulePath, err)
	}
	ruleRoot := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml":  string(raw),
	})
	cfg, err := config.Load(ruleRoot)
	if err != nil {
		t.Fatalf("%s does not load: %v", blankDeclarationRulePath, err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("%s declares %d rules, want 1", blankDeclarationRulePath, len(cfg.Rules))
	}
	rule := cfg.Rules[0]

	// wantFire is stated only for the class the loader cannot judge: a value
	// that decodes to "". Everywhere else the loader IS the expectation.
	cases := []struct {
		name     string
		decl     string
		wantFire *bool // nil unless the loader reports undeclared
	}{
		{name: "three spaces, quoted", decl: "    fixture_exempt: \"   \"\n"},
		{name: "one space, single-quoted", decl: "    fixture_exempt: ' '\n"},
		{name: "a literal tab, quoted", decl: "    fixture_exempt: \"\t\"\n"},
		{name: "quoted spaces then a comment", decl: "    fixture_exempt: \"  \" # no reason\n"},
		{name: "a tab spelled as a double-quoted escape", decl: `    fixture_exempt: "\t"` + "\n"},
		{name: "a newline spelled as a double-quoted escape", decl: `    fixture_exempt: "\n"` + "\n"},
		{name: "the same escape inside single quotes, where a backslash is literal", decl: `    fixture_exempt: '\t'` + "\n"},
		{name: "a real folded reason", decl: "    fixture_exempt: >-\n      The detector shells out to a formatter, which no fixture tree can carry.\n"},
		{name: "a real reason after a blank line", decl: "    fixture_exempt: >-\n\n      The detector shells out to a formatter, which no fixture tree can carry.\n"},
		{name: "a real plain scalar", decl: "    fixture_exempt: the detector shells out\n"},
		{name: "a folded body that is only a comment", decl: "    fixture_exempt: >-\n      # still content\n"},

		// The undeclared class: every one of these decodes to "".
		{name: "the empty scalar", decl: "    fixture_exempt: \"\"\n", wantFire: boolp(false)},
		{name: "the empty single-quoted scalar", decl: "    fixture_exempt: ''\n", wantFire: boolp(false)},
		{name: "the key with no value at all", decl: "    fixture_exempt:\n", wantFire: boolp(false)},
		{name: "a folded scalar with no body", decl: "    fixture_exempt: >-\n", wantFire: boolp(true)},
		{name: "a literal scalar with no body", decl: "    fixture_exempt: |\n", wantFire: boolp(true)},
		{name: "a folded scalar whose body is blank", decl: "    fixture_exempt: >-\n\n", wantFire: boolp(true)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader := askLoader(t, tc.decl)
			fires := askRule(t, rule, tc.decl)
			switch loader {
			case loaderDeclared:
				if fires {
					t.Errorf("the loader stores this as a reason and %s reports it as declaring nothing.\n"+
						"Two halves of one field disagreeing is #336 with the operands swapped: a rule\n"+
						"author would be refused on the content surface and accepted on the config one.", rule.ID)
				}
			case loaderRefused:
				if !fires {
					t.Errorf("the loader refuses this value (exit 2) and %s is silent on it.\n"+
						"That refusal only reaches a file this binary loads as its own config; the\n"+
						"governed-content surface — a ported corpus, a vendored subproject, a downstream\n"+
						"repo on an older binary — has this rule and nothing else.", rule.ID)
				}
			case loaderUndeclared:
				if tc.wantFire == nil {
					t.Fatalf("the loader reports this decl undeclared and the case states no expectation;\n" +
						"the undeclared class is the one the loader cannot judge, so it must be tabled")
				}
				if fires != *tc.wantFire {
					t.Errorf("%s fires=%v on a value the loader reads as undeclared; want %v.\n"+
						"An empty scalar is the undeclared state written out and fixture-coverage\n"+
						"reports it; a block-scalar header with no body is the 68-file corpus idiom\n"+
						"with its reason deleted, and reads as a declaration to everyone but the parser.",
						rule.ID, fires, *tc.wantFire)
				}
			}
			if testing.Verbose() {
				t.Logf("loader=%s rule-fires=%v", loader, fires)
			}
		})
	}
}

func boolp(b bool) *bool { return &b }
