package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// heavyChecker is a Coster that declares CostHeavy, for cost-filter tests.
type heavyChecker struct{}

func (heavyChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }
func (heavyChecker) Cost() rules.Cost                            { return rules.CostHeavy }

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const validRoot = "version: 1\n"

const validRules = `rules:
  - id: no-direct-pool
    type: forbidden-pattern
    severity: error
    scope:
      include: ["src/**/*.go"]
      exclude: ["**/*_test.go"]
    params:
      pattern: 'pgxpool\.New\('
    except:
      paths: ["src/db/**"]
    cure: "Use internal/db."
  - id: has-readme-marker
    type: required-pattern
    scope:
      include: ["README.md"]
    params:
      pattern: 'formwork'
      mode: exists
`

func TestLoadCompilesRulesSortedByID(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   validRoot,
		".formwork/rules/main.yaml": validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || len(cfg.Rules) != 2 {
		t.Fatalf("cfg: version=%d rules=%d", cfg.Version, len(cfg.Rules))
	}
	if cfg.Rules[0].ID != "has-readme-marker" || cfg.Rules[1].ID != "no-direct-pool" {
		t.Fatalf("rules not sorted by id: %s, %s", cfg.Rules[0].ID, cfg.Rules[1].ID)
	}
	r := cfg.Rules[1]
	if r.Severity != finding.SeverityError || r.Cure != "Use internal/db." || r.Checker == nil {
		t.Fatalf("rule not compiled: %+v", r)
	}
}

func TestLoadParsesEngineConstraint(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nengine: \">=0.2.0\"\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Engine != ">=0.2.0" {
		t.Fatalf("Engine = %q, want \">=0.2.0\"", cfg.Engine)
	}
	if cfg.EngineConstraint == nil {
		t.Fatal("EngineConstraint is nil, want a parsed constraint")
	}
	if !cfg.EngineConstraint.Check(semver.MustParse("v0.3.0")) {
		t.Fatal("expected v0.3.0 to satisfy >=0.2.0")
	}
}

func TestLoadNoEngineFieldIsNil(t *testing.T) {
	root := writeRepo(t, map[string]string{".formwork/formwork.yaml": validRoot})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Engine != "" || cfg.EngineConstraint != nil {
		t.Fatalf("expected no engine, got Engine=%q EngineConstraint=%v", cfg.Engine, cfg.EngineConstraint)
	}
}

// TestReadEnvelopeIndependentOfRuleFiles is the envelope's whole reason to
// exist: the engine backstop must be checkable before rule files are parsed,
// so ReadEnvelope must succeed even when a rule file is unparseable or
// declares a type this binary does not know.
func TestReadEnvelopeIndependentOfRuleFiles(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nengine: \">=0.2.0\"\n",
		".formwork/rules/r.yaml":  "rules:\n  - id: a-rule\n    type: brand-new-type\n    scope: {include: ['**']}\n",
	})
	env, err := config.ReadEnvelope(root)
	if err != nil {
		t.Fatalf("ReadEnvelope returned an error even though only formwork.yaml should be read: %v", err)
	}
	if env.Version != 1 {
		t.Fatalf("Version = %d, want 1", env.Version)
	}
	if env.Engine != ">=0.2.0" {
		t.Fatalf("Engine = %q, want \">=0.2.0\"", env.Engine)
	}
	if env.EngineConstraint == nil || !env.EngineConstraint.Check(semver.MustParse("v0.3.0")) {
		t.Fatalf("EngineConstraint = %v, want a parsed constraint satisfied by v0.3.0", env.EngineConstraint)
	}
	// Confirm the rule file really is unparseable by this binary — otherwise
	// this test proves nothing about independence from rule files. Goes
	// through the same envelope LoadRules parses, exercising the two-step
	// path (not just config.Load's wrapper).
	if _, err := env.LoadRules(); err == nil || !strings.Contains(err.Error(), "brand-new-type") {
		t.Fatalf("expected LoadRules to fail on the unknown rule type, got: %v", err)
	}
}

func TestReadEnvelopeRejectsBadEngineConstraint(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nengine: \"nonsense\"\n",
	})
	_, err := config.ReadEnvelope(root)
	if err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatalf("want error containing \"engine\", got %v", err)
	}
}

func TestReadEnvelopeRejectsBadVersion(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 2\n",
	})
	_, err := config.ReadEnvelope(root)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("want error containing \"version\", got %v", err)
	}
}

// TestLoadRulesUsesSameEnvelopeBytesAsGate is finding 8's regression guard:
// a caller reads the envelope once, gates on it, then calls LoadRules — the
// rules that get compiled must come from that same Envelope value, not a
// fresh re-read of formwork.yaml that could have changed underneath it.
// Mutating the file on disk after ReadEnvelope, before LoadRules, proves
// LoadRules never goes back to disk for the envelope itself: engine.
func TestLoadRulesUsesSameEnvelopeBytesAsGate(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nengine: \">=0.2.0\"\n",
		".formwork/rules/r.yaml":  validRules,
	})
	env, err := config.ReadEnvelope(root)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite formwork.yaml with a constraint that would refuse everything,
	// simulating a concurrent edit between the gate and rule parsing.
	rootPath := filepath.Join(root, ".formwork", "formwork.yaml")
	if err := os.WriteFile(rootPath, []byte("version: 1\nengine: \"<0.0.1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := env.LoadRules()
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if cfg.Engine != ">=0.2.0" {
		t.Fatalf("Engine = %q, want the envelope's original \">=0.2.0\" (LoadRules must not re-read formwork.yaml)", cfg.Engine)
	}
}

func TestAppliesHonorsIncludeExcludeExcept(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   validRoot,
		".formwork/rules/main.yaml": validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Rules[1] // no-direct-pool
	cases := []struct {
		path string
		want bool
	}{
		{"src/api/handler.go", true},
		{"src/api/handler_test.go", false}, // exclude
		{"src/db/pool.go", false},          // except.paths
		{"cmd/main.go", false},             // outside include
	}
	for _, c := range cases {
		if got := r.Applies(c.path); got != c.want {
			t.Errorf("Applies(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	base := map[string]string{".formwork/formwork.yaml": validRoot}
	cases := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{"unknown envelope field", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: a-rule\n    type: forbidden-pattern\n    scoop: {}\n    scope: {include: ['**']}\n    params: {pattern: x}\n",
		}, "scoop"},
		{"unknown type", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: a-rule\n    type: mystery\n    scope: {include: ['**']}\n",
		}, "mystery"},
		{"duplicate id", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: a-rule\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: x}\n  - id: a-rule\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: y}\n",
		}, "duplicate"},
		{"bad id casing", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: Bad_ID\n    type: forbidden-pattern\n    scope: {include: ['**']}\n    params: {pattern: x}\n",
		}, "kebab-case"},
		{"empty include", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: a-rule\n    type: forbidden-pattern\n    scope: {include: []}\n    params: {pattern: x}\n",
		}, "scope.include"},
		{"bad severity", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: a-rule\n    type: forbidden-pattern\n    severity: fatal\n    scope: {include: ['**']}\n    params: {pattern: x}\n",
		}, "severity"},
		{"bad glob", map[string]string{
			".formwork/rules/r.yaml": "rules:\n  - id: a-rule\n    type: forbidden-pattern\n    scope: {include: ['[']}\n    params: {pattern: x}\n",
		}, "glob"},
		{"bad engine constraint", map[string]string{
			".formwork/formwork.yaml": "version: 1\nengine: \"nonsense\"\n",
		}, "engine"},
		{"wrong version", map[string]string{".formwork/formwork.yaml": "version: 2\n"}, "version"},
		{"missing formwork.yaml", map[string]string{".formwork/rules/r.yaml": "rules: []\n"}, "formwork.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range base {
				files[k] = v
			}
			for k, v := range c.files {
				files[k] = v
			}
			if c.name == "wrong version" || c.name == "missing formwork.yaml" {
				files = c.files
			}
			_, err := config.Load(writeRepo(t, files))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestLoadAllowsZeroRules(t *testing.T) {
	root := writeRepo(t, map[string]string{".formwork/formwork.yaml": validRoot})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 0 {
		t.Fatalf("rules: %d", len(cfg.Rules))
	}
}

const lanedRoot = `version: 1
lanes:
  pre-commit:
    tags: [go]
  ci:
    all: true
    ci: true
`

const lanedRules = `rules:
  - id: dart-rule
    type: forbidden-pattern
    scope: {include: ['**/*.dart']}
    params: {pattern: TODO}
    tags: [dart]
  - id: go-rule
    type: forbidden-pattern
    scope: {include: ['**/*.go']}
    params: {pattern: TODO}
    tags: [go]
`

func TestLoadParsesLanesSortedByName(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   lanedRoot,
		".formwork/rules/main.yaml": lanedRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Lanes) != 2 {
		t.Fatalf("lanes: %d", len(cfg.Lanes))
	}
	// Sorted by Name: "ci" sorts before "pre-commit".
	if cfg.Lanes[0].Name != "ci" || cfg.Lanes[1].Name != "pre-commit" {
		t.Fatalf("lanes not sorted by name: %s, %s", cfg.Lanes[0].Name, cfg.Lanes[1].Name)
	}
	ci := cfg.Lanes[0]
	if !ci.All || !ci.CI || len(ci.Tags) != 0 {
		t.Fatalf("ci lane not parsed: %+v", ci)
	}
	pc := cfg.Lanes[1]
	if pc.All || pc.CI || len(pc.Tags) != 1 || pc.Tags[0] != "go" {
		t.Fatalf("pre-commit lane not parsed: %+v", pc)
	}
}

func TestLoadWithoutLanesLeavesLanesEmpty(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   validRoot,
		".formwork/rules/main.yaml": validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Lanes) != 0 {
		t.Fatalf("expected no lanes, got %d", len(cfg.Lanes))
	}
}

func TestLaneSelects(t *testing.T) {
	all := config.Lane{Name: "ci", All: true, CI: true}
	goLane := config.Lane{Name: "pre-commit", Tags: []string{"go"}}
	goRule := &config.Rule{ID: "go-rule", Tags: []string{"go"}}
	dartRule := &config.Rule{ID: "dart-rule", Tags: []string{"dart"}}
	multiRule := &config.Rule{ID: "multi-rule", Tags: []string{"dart", "go"}}
	noTags := &config.Rule{ID: "misc"}

	// all selects every rule regardless of tags.
	for _, r := range []*config.Rule{goRule, dartRule, noTags} {
		if !all.Selects(r) {
			t.Errorf("all lane should select %s", r.ID)
		}
	}
	// tag lane selects only rules sharing a tag.
	if !goLane.Selects(goRule) {
		t.Error("go lane should select go-rule")
	}
	if !goLane.Selects(multiRule) {
		t.Error("go lane should select multi-rule (intersection)")
	}
	if goLane.Selects(dartRule) {
		t.Error("go lane should not select dart-rule (no intersection)")
	}
	if goLane.Selects(noTags) {
		t.Error("go lane should not select an untagged rule")
	}
}

func TestLaneCostFilter(t *testing.T) {
	fastRule := &config.Rule{ID: "fast-rule", Tags: []string{"go"}} // nil checker → fast
	heavyRule := &config.Rule{ID: "heavy-rule", Tags: []string{"go"}, Checker: heavyChecker{}}
	if fastRule.Cost() != rules.CostFast {
		t.Fatalf("nil-checker rule should be fast, got %q", fastRule.Cost())
	}
	if heavyRule.Cost() != rules.CostHeavy {
		t.Fatalf("heavy-checker rule should be heavy, got %q", heavyRule.Cost())
	}

	fastLane := config.Lane{Name: "pre-commit", Tags: []string{"go"}, Cost: "fast"}
	heavyLane := config.Lane{Name: "pre-push", Tags: []string{"go"}, Cost: "heavy"}
	anyLane := config.Lane{Name: "ci", All: true} // no cost → any

	if !fastLane.Selects(fastRule) || fastLane.Selects(heavyRule) {
		t.Error("fast lane should select only the fast rule")
	}
	if !heavyLane.Selects(heavyRule) || heavyLane.Selects(fastRule) {
		t.Error("heavy lane should select only the heavy rule")
	}
	if !anyLane.Selects(fastRule) || !anyLane.Selects(heavyRule) {
		t.Error("any-cost lane should select both")
	}
}

func TestConfigLaneLookup(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   lanedRoot,
		".formwork/rules/main.yaml": lanedRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := cfg.Lane("ci"); !ok || !l.All {
		t.Fatalf("Lane(ci) = %+v, %v", l, ok)
	}
	if _, ok := cfg.Lane("bogus"); ok {
		t.Fatal("Lane(bogus) should report not found")
	}
}

func TestRulesForLane(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml":   lanedRoot,
		".formwork/rules/main.yaml": lanedRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.RulesForLane("bogus"); err == nil {
		t.Fatal("RulesForLane(bogus) should error on unknown lane")
	}
	pc, err := cfg.RulesForLane("pre-commit")
	if err != nil {
		t.Fatal(err)
	}
	if len(pc) != 1 || pc[0].ID != "go-rule" {
		t.Fatalf("pre-commit rules = %v", ruleIDs(pc))
	}
	ci, err := cfg.RulesForLane("ci")
	if err != nil {
		t.Fatal(err)
	}
	// all lane returns every rule, preserving the ID-sorted order.
	if len(ci) != 2 || ci[0].ID != "dart-rule" || ci[1].ID != "go-rule" {
		t.Fatalf("ci rules = %v", ruleIDs(ci))
	}
}

func ruleIDs(rls []*config.Rule) []string {
	ids := make([]string, len(rls))
	for i, r := range rls {
		ids[i] = r.ID
	}
	return ids
}

func TestLoadRejectsBadLanes(t *testing.T) {
	cases := []struct {
		name, root, wantErr string
	}{
		{"all and tags together", "version: 1\nlanes:\n  x:\n    all: true\n    tags: [go]\n", "selector"},
		{"neither selector", "version: 1\nlanes:\n  x:\n    ci: true\n", "selector"},
		{"unknown lane field", "version: 1\nlanes:\n  x:\n    all: true\n    frob: 1\n", "frob"},
		{"bad lane name casing", "version: 1\nlanes:\n  Bad_Lane:\n    all: true\n", "kebab-case"},
		{"invalid cost", "version: 1\nlanes:\n  x:\n    all: true\n    cost: slow\n", "cost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := writeRepo(t, map[string]string{".formwork/formwork.yaml": c.root})
			_, err := config.Load(root)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestLoadLaneErrorIsDeterministic guards the deterministic-error discipline:
// with more than one invalid lane, the reported error must always name the
// alphabetically-first lane ("aaa"), never depend on map iteration order.
// Loaded repeatedly because the bug it guards is probabilistic (map order).
func TestLoadLaneErrorIsDeterministic(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nlanes:\n  zzz:\n    all: true\n    tags: [go]\n  aaa:\n    ci: true\n",
	})
	for i := 0; i < 20; i++ {
		_, err := config.Load(root)
		if err == nil || !strings.Contains(err.Error(), "lane aaa") {
			t.Fatalf("run %d: want deterministic error naming lane aaa, got %v", i, err)
		}
	}
}

func TestScopeClassify(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n" +
			"scope:\n" +
			"  docs: ['**/*.md', 'docs/**']\n" +
			"  governance: ['.formwork/**', '.github/**']\n" +
			"  languages:\n" +
			"    go: ['**/*.go', '**/*.sql']\n" +
			"    dart: ['**/*.dart']\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	sc := cfg.Scope

	cases := []struct {
		name    string
		changed []string
		want    string
		go_, dt bool
	}{
		{"empty is docs", nil, "docs", false, false},
		{"docs only", []string{"README.md", "docs/x.md"}, "docs", false, false},
		{"governance beats docs", []string{"README.md", ".github/ci.yml"}, "governance", false, false},
		{"runtime wins", []string{"README.md", "internal/a.go"}, "runtime", true, false},
		{"unclassified is runtime", []string{"weird.bin"}, "runtime", false, false},
		{"dart flag", []string{"lib/main.dart"}, "runtime", false, true},
		{"sql sets go flag", []string{"db/x.sql", "docs/y.md"}, "runtime", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			class, langs := sc.Classify(c.changed)
			if class != c.want {
				t.Errorf("class = %q, want %q", class, c.want)
			}
			if langs["go"] != c.go_ || langs["dart"] != c.dt {
				t.Errorf("flags go=%v dart=%v, want go=%v dart=%v", langs["go"], langs["dart"], c.go_, c.dt)
			}
		})
	}
}

func TestScopeInvalidGlobRejected(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscope:\n  docs: ['[']\n",
	})
	if _, err := config.Load(root); err == nil || !strings.Contains(err.Error(), "scope.docs") {
		t.Fatalf("want scope.docs glob error, got %v", err)
	}
}

func TestExcludeEntriesCaptureYAMLComments(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope:\n      include: ['**/*.txt']\n      exclude:\n" +
			"        - 'vendor/**' # preventative: vendored trees\n" +
			"        - '**/*_test.go'\n" +
			"    params: {pattern: banana}\n",
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("rules: %d", len(cfg.Rules))
	}
	entries := cfg.Rules[0].ExcludeEntries()
	if len(entries) != 2 {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[0].Glob != "vendor/**" || entries[0].Comment == "" {
		t.Fatalf("entry0 want commented vendor/**, got %+v", entries[0])
	}
	if !strings.Contains(entries[0].Comment, "preventative") {
		t.Fatalf("entry0 comment: %q", entries[0].Comment)
	}
	if entries[1].Glob != "**/*_test.go" || entries[1].Comment != "" {
		t.Fatalf("entry1 want uncommented **/*_test.go, got %+v", entries[1])
	}
}

func TestLoadScanIgnoreParsed(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  ignore:\n    - glob: '.claude/worktrees/**'\n      reason: agent harness worktrees\n    - glob: 'vendor/**'\n      reason: vendored source\n",
		".formwork/rules/r.yaml":  validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []config.IgnoreEntry{
		{Glob: ".claude/worktrees/**", Reason: "agent harness worktrees"},
		{Glob: "vendor/**", Reason: "vendored source"},
	}
	if !reflect.DeepEqual(cfg.Ignore, want) {
		t.Fatalf("Ignore = %#v, want %#v", cfg.Ignore, want)
	}
	if got := cfg.IgnoreGlobs(); !reflect.DeepEqual(got, []string{".claude/worktrees/**", "vendor/**"}) {
		t.Fatalf("IgnoreGlobs = %#v", got)
	}
}

func TestLoadScanGitignoreParsed(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  gitignore:\n    reason: git already refuses these\n",
		".formwork/rules/r.yaml":  validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gitignore == nil {
		t.Fatal("Gitignore = nil, want the declaration")
	}
	if cfg.Gitignore.Reason != "git already refuses these" {
		t.Fatalf("Reason = %q", cfg.Gitignore.Reason)
	}
}

// Absent means off, and off must be the zero-cost default: a repo that never
// heard of this key gets exactly the walk it got before.
func TestLoadScanGitignoreAbsentIsNil(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: why\n",
		".formwork/rules/r.yaml":  validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gitignore != nil {
		t.Fatalf("Gitignore = %#v, want nil when the key is absent", cfg.Gitignore)
	}
}

// A bare `gitignore:` with no body reads as absent — off — and that is a
// deliberate choice made against the alternative, not an oversight.
//
// yaml.v3 decodes a null mapping value into a nil pointer, so the only way to
// tell `gitignore:` from a missing key is to decode the field as a yaml.Node
// and inspect its tag. Measured: yaml.Node.Decode does NOT honour
// KnownFields, so taking that route would buy the rejection of an empty block
// at the price of silently accepting `frob: 1` inside a populated one. A typo'd
// field that is ignored is the worse failure of the two — it looks configured
// and is not — and an empty block errs toward pruning NOTHING, which cannot
// make a rule pass that would otherwise fail. Pinned so the trade stays a
// decision rather than drifting back.
func TestLoadScanGitignoreNullBodyIsOff(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  gitignore:\n",
		".formwork/rules/r.yaml":  validRules,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gitignore != nil {
		t.Fatalf("Gitignore = %#v, want nil (a bodyless key prunes nothing)", cfg.Gitignore)
	}
}

func TestLoadRejectsBadScanGitignore(t *testing.T) {
	cases := []struct{ name, root, wantErr string }{
		{"missing reason", "version: 1\nscan:\n  gitignore: {}\n", "reason is required"},
		{"blank reason", "version: 1\nscan:\n  gitignore:\n    reason: '  '\n", "reason is required"},
		{"unknown field", "version: 1\nscan:\n  gitignore:\n    reason: why\n    frob: 1\n", "frob"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := writeRepo(t, map[string]string{
				".formwork/formwork.yaml": c.root,
				".formwork/rules/r.yaml":  validRules,
			})
			_, err := config.Load(root)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want contains %q", err, c.wantErr)
			}
		})
	}
}

func TestLoadRejectsBadScanIgnore(t *testing.T) {
	cases := []struct{ name, root, wantErr string }{
		{"missing reason", "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n", "reason is required"},
		{"blank reason", "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: '   '\n", "reason is required"},
		{"missing glob", "version: 1\nscan:\n  ignore:\n    - reason: why\n", "glob is required"},
		{"invalid glob", "version: 1\nscan:\n  ignore:\n    - glob: '['\n      reason: why\n", "invalid glob"},
		{"absolute glob", "version: 1\nscan:\n  ignore:\n    - glob: '/vendor/**'\n      reason: why\n", "repo-relative"},
		{"dot-slash glob", "version: 1\nscan:\n  ignore:\n    - glob: './vendor/**'\n      reason: why\n", "repo-relative"},
		{"unknown scan sub-key", "version: 1\nscan:\n  frob: 1\n", "frob"},
		{"unknown ignore entry field", "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: why\n      frob: 1\n", "frob"},
		// Fills a pre-existing gap: nothing pinned top-level formwork.yaml strictness.
		{"unknown top-level key", "version: 1\nfrob: 1\n", "frob"},
		// yaml.v3 silently DROPS !!null sequence items when decoding into a
		// value-struct slice, so `- null` would vanish instead of erroring —
		// a strict-decoding fail-open two characters away from `- {}`, which
		// errors. Pointer-slice decoding preserves the null for rejection.
		{"null entry", "version: 1\nscan:\n  ignore:\n    - null\n", "scan.ignore"},
		{"bare dash entry", "version: 1\nscan:\n  ignore:\n    -\n", "scan.ignore"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := writeRepo(t, map[string]string{
				".formwork/formwork.yaml": c.root,
				".formwork/rules/r.yaml":  validRules,
			})
			_, err := config.Load(root)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want contains %q", err, c.wantErr)
			}
		})
	}
}
