// scopefloor_test.go — the scope.min_files floor (#23). Separate from
// lint_test.go, which the 750-line vendor cap bounds; same package.
package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// floorTree walks a repo of three .go files and one .md, so a scope can select
// 3, 1, or 0 of them by glob alone.
func floorTree(t *testing.T) []*scan.File {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"a.go", "b.go", "c.go", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return fset.Files
}

// heavyPlainChecker is what makes a rule "external tool" to meta: the
// classification is COST-based (isExternalTool == Cost() heavy), not a match on
// the type string, so a rule declared `type: command` with a fast checker is not
// one. Using the real predicate's input is what keeps the exemption control in
// TestScopeFloorFindingsAppliesToExternalToolRules honest.
type heavyPlainChecker struct{}

func (heavyPlainChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (heavyPlainChecker) Cost() rules.Cost                            { return rules.CostHeavy }

func floorRule(t *testing.T, id, typeName, include string, floor int) *config.Rule {
	t.Helper()
	var c rules.Checker = plainChecker{}
	if typeName == "command" {
		c = heavyPlainChecker{}
	}
	r, err := config.New(id, typeName, finding.SeverityError, "", []string{include}, nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetMinFiles(floor); err != nil {
		t.Fatal(err)
	}
	return r
}

// The default is the whole point: #160 made an empty scope disclosed but still a
// pass, deliberately, and every rule in every corpus carries the unset floor. A
// rule that declares no floor must be untouched by this even when its scope
// selects NOTHING — which is the strongest form of the back-compat claim.
func TestScopeFloorFindingsSilentWithoutAFloor(t *testing.T) {
	files := floorTree(t)
	rls := []*config.Rule{
		floorRule(t, "matches-nothing", "forbidden-pattern", "**/*.dart", 0),
		floorRule(t, "matches-all", "forbidden-pattern", "**/*.go", 0),
	}
	if got := meta.ScopeFloorFindings(rls, files); len(got) != 0 {
		t.Fatalf("got %+v, want no findings — an undeclared floor must not change any verdict", got)
	}
}

func TestScopeFloorFindingsReportsTheShortfall(t *testing.T) {
	files := floorTree(t)
	rls := []*config.Rule{floorRule(t, "go-corpus", "forbidden-pattern", "**/*.md", 3)}

	got := meta.ScopeFloorFindings(rls, files)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(got), got)
	}
	f := got[0]
	if f.RuleID != "go-corpus" {
		t.Errorf("RuleID = %q, want go-corpus", f.RuleID)
	}
	// Error severity regardless of the rule's own severity: the floor is the
	// operator's assertion about the corpus, and a warn-severity shortfall would
	// exit 0 — an armed floor that cannot fail the run.
	if f.Severity != finding.SeverityError {
		t.Errorf("Severity = %q, want error", f.Severity)
	}
	// Scope-level findings carry no path (finding.Finding's documented contract),
	// which is also what makes them unexemptable (spec §5).
	if f.Path != "" || f.Line != 0 {
		t.Errorf("want a scope-level finding with no path/line, got %q:%d", f.Path, f.Line)
	}
	for _, want := range []string{"scope.min_files", "matched 1 file(s)", "floor of 3"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message %q must name %q — the shortfall is the whole cure surface", f.Message, want)
		}
	}
}

// The boundary, in both directions from the same tree: exactly-at-the-floor
// passes, one below fails. Without the pass half, a floor implemented as `<=`
// would satisfy the failing test above.
func TestScopeFloorFindingsBoundary(t *testing.T) {
	files := floorTree(t)
	if got := meta.ScopeFloorFindings([]*config.Rule{floorRule(t, "exact", "forbidden-pattern", "**/*.go", 3)}, files); len(got) != 0 {
		t.Errorf("3 files against a floor of 3 must pass, got %+v", got)
	}
	if got := meta.ScopeFloorFindings([]*config.Rule{floorRule(t, "one-short", "forbidden-pattern", "**/*.go", 4)}, files); len(got) != 1 {
		t.Errorf("3 files against a floor of 4 must fail, got %+v", got)
	}
}

// The floor counts the file set it is GIVEN, and nothing else — it never
// re-walks. That is what lets the caller decide which set a mode's floor is a
// claim about: `check --staged` hands it the tracked tree, whole-tree hands it
// the walk. The same rule against the same repo answers differently on the two
// sets, and this pins that the choice lives with the caller rather than here.
func TestScopeFloorFindingsCountsOnlyTheFilesGiven(t *testing.T) {
	files := floorTree(t)
	rls := []*config.Rule{floorRule(t, "go-corpus", "forbidden-pattern", "**/*.go", 3)}

	if got := meta.ScopeFloorFindings(rls, files); len(got) != 0 {
		t.Fatalf("3 .go files meet the floor, got %+v", got)
	}
	// The same rule over a subset — what a tracked-tree restriction produces
	// when the corpus is on disk but out of the index.
	if got := meta.ScopeFloorFindings(rls, files[:1]); len(got) != 1 {
		t.Fatalf("a restricted set below the floor must report, got %+v", got)
	}
	if got := meta.ScopeFloorFindings(rls, nil); len(got) != 1 {
		t.Fatalf("an empty set must report, got %+v", got)
	}
}

// AnyScopeFloor is the cheap question `check` asks before paying for the tracked
// tree in a file-set run. It must not answer yes for an unarmed corpus — that
// would put a git call on the pre-commit path of every repo that never declared
// a floor.
func TestAnyScopeFloor(t *testing.T) {
	unarmed := []*config.Rule{floorRule(t, "a", "forbidden-pattern", "**/*.go", 0)}
	if meta.AnyScopeFloor(unarmed) {
		t.Error("an unarmed corpus must answer no")
	}
	if meta.AnyScopeFloor(nil) {
		t.Error("an empty rule set must answer no")
	}
	armed := append(unarmed, floorRule(t, "b", "forbidden-pattern", "**/*.go", 1))
	if !meta.AnyScopeFloor(armed) {
		t.Error("one armed rule is enough")
	}
}

// The deliberate divergence from RulesMatchingNoFiles, which exempts
// external-tool rules because it DIAGNOSES every rule automatically and their
// verdict does not come from their scope. A min_files floor is not automatic: it
// is a per-rule value the operator typed. Honouring it on a command rule is the
// fail-closed reading, and silently ignoring a number someone wrote is the
// failure mode this repo keeps finding.
func TestScopeFloorFindingsAppliesToExternalToolRules(t *testing.T) {
	files := floorTree(t)
	rls := []*config.Rule{floorRule(t, "shell-out", "command", "**/*.dart", 1)}
	if got := meta.ScopeFloorFindings(rls, files); len(got) != 1 {
		t.Fatalf("got %+v, want the explicitly-armed floor honoured on a command rule", got)
	}
	// The control: the automatic diagnosis still exempts the same rule, so this
	// test cannot pass by the exemption having been deleted wholesale.
	if ids := meta.RulesMatchingNoFiles(rls, files); len(ids) != 0 {
		t.Fatalf("RulesMatchingNoFiles = %v, want external-tool rules still exempt there", ids)
	}
}

// Order is the order the rules were given, matching RulesMatchingNoFiles.
// Callers sort findings by (rule, path, line) before rendering; this pins that
// the seam itself does not shuffle.
func TestScopeFloorFindingsPreservesRuleOrder(t *testing.T) {
	files := floorTree(t)
	rls := []*config.Rule{
		floorRule(t, "zeta", "forbidden-pattern", "**/*.dart", 1),
		floorRule(t, "alpha", "forbidden-pattern", "**/*.dart", 1),
	}
	got := meta.ScopeFloorFindings(rls, files)
	if len(got) != 2 || got[0].RuleID != "zeta" || got[1].RuleID != "alpha" {
		t.Fatalf("got %+v, want [zeta alpha] in rule order", got)
	}
}
