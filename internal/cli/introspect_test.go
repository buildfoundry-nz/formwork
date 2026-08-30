package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// probeChecker exists so a test-only rule type can be registered.
type probeChecker struct{}

func (probeChecker) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

// init registers a type the shipped binary does not know. list must show it
// with zero list-side edits — proving the registry is the single enumeration
// source, not a hand-maintained literal. Registration happens at test-binary
// init, honoring the registry's "never Register after startup" contract.
func init() {
	rules.Register("test/list-probe", func(*yaml.Node) (rules.Checker, error) {
		return probeChecker{}, nil
	})
}

func TestListTypesEnumeratesRegistry(t *testing.T) {
	code, out, _ := runCLI(t, "list", "types")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if want := len(rules.TypeNames()); len(lines) != want {
		t.Fatalf("listed %d types, registry has %d:\n%s", len(lines), want, out)
	}
	for _, needed := range []string{"forbidden-pattern", "sql/statement-predicate", "test/list-probe"} {
		if !strings.Contains(out, needed) {
			t.Fatalf("list types missing %q:\n%s", needed, out)
		}
	}
}

func TestListPreprocessorsIncludesRawAndVariants(t *testing.T) {
	code, out, _ := runCLI(t, "list", "preprocessors")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if want := len(preprocess.Names()); len(lines) != want {
		t.Fatalf("listed %d preprocessors, registry has %d:\n%s", len(lines), want, out)
	}
	for _, needed := range []string{"raw", "decomment-go"} {
		if !strings.Contains(out, needed) {
			t.Fatalf("list preprocessors missing %q:\n%s", needed, out)
		}
	}
}

func TestListRulesShowsIdTypeSeverityCost(t *testing.T) {
	code, out, _ := runCLI(t, "list", "-C", filepath.Join("testdata", "toyrepo"), "rules")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rules, got %d:\n%s", len(lines), out)
	}
	// Sorted by id, and each line carries id, type, severity, cost.
	if !strings.Contains(lines[0], "no-todo-markers") || !strings.Contains(lines[1], "readme-mentions-formwork") {
		t.Fatalf("rules not sorted by id:\n%s", out)
	}
	for _, needed := range []string{"forbidden-pattern", "error", "fast"} {
		if !strings.Contains(lines[0], needed) {
			t.Fatalf("first rule line missing %q: %s", needed, lines[0])
		}
	}
}

func TestListLanesShowsSelectors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
lanes:
  ci:
    all: true
    ci: true
  docs:
    tags: [docs]
    cost: fast
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: r1
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'x'
    tags: [docs]
`)
	code, out, _ := runCLI(t, "list", "-C", root, "lanes")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "ci") || !strings.Contains(out, "docs") {
		t.Fatalf("lanes missing:\n%s", out)
	}
	if !strings.Contains(out, "all") || !strings.Contains(out, "tags") {
		t.Fatalf("selectors missing:\n%s", out)
	}
}

func TestListUnknownKindExits2(t *testing.T) {
	code, _, errOut := runCLI(t, "list", "gadgets")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "gadgets") {
		t.Fatalf("error must name the unknown kind: %s", errOut)
	}
}

func TestListRulesMissingConfigExits2(t *testing.T) {
	code, _, _ := runCLI(t, "list", "-C", t.TempDir(), "rules")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestListUnknownFormatExits2(t *testing.T) {
	code, _, errOut := runCLI(t, "list", "-format", "xml", "types")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "xml") {
		t.Fatalf("error must name the bad format: %s", errOut)
	}
}

func TestListJSONIsStableAndParses(t *testing.T) {
	code, out, _ := runCLI(t, "list", "-C", filepath.Join("testdata", "toyrepo"), "-format", "json", "rules")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var got []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Severity string `json:"severity"`
		Cost     string `json:"cost"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0].ID != "no-todo-markers" || got[0].Type != "forbidden-pattern" || got[0].Severity != "error" || got[0].Cost != "fast" {
		t.Fatalf("unexpected json: %+v", got)
	}
}

// writeFile writes rel under root, creating parents.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// explainRepo builds a repo whose one fully-loaded rule exercises every field
// explain must render, plus a second bare rule with no fixtures.
func explainRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: guard-pool
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
      exclude:
        # generated code is exempt by policy
        - "gen/**"
    preprocess: strings-only-go
    params:
      pattern: 'pgxpool\.New'
    except:
      paths: ["cmd/legacy/main.go"]
      marker: true
      allowlist: allowlists/pool.txt
    cure: "Use internal/db.Pool."
    origin: "#42"
    tags: [go]
  - id: bare-rule
    type: forbidden-pattern
    severity: warn
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'HACK'
`)
	writeFile(t, root, ".formwork/allowlists/pool.txt", "internal/old/db.go\ninternal/old/tx.go\n")
	writeFile(t, root, ".formwork/fixtures/guard-pool/fire-1/x.go", "package x\n")
	writeFile(t, root, ".formwork/fixtures/guard-pool/pass-1/x.go", "package x\n")
	writeFile(t, root, ".formwork/fixtures/bare-rule/fire-1/x.txt", "HACK\n")
	return root
}

func TestExplainPrintsRuleInFull(t *testing.T) {
	root := explainRepo(t)
	code, out, _ := runCLI(t, "explain", "-C", root, "guard-pool")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, needed := range []string{
		"guard-pool", "forbidden-pattern", "error",
		"**/*.go",                  // scope.include
		"gen/**",                   // scope.exclude
		"generated code is exempt", // the exclude's justification comment
		"strings-only-go",          // preprocess
		"pattern",                  // param name
		`pgxpool\.New`,             // param value
		"cmd/legacy/main.go",       // except.paths
		"marker",                   // marker suppression enabled
		"allowlists/pool.txt",      // allowlist file
		"2 entr",                   // allowlist entry count
		"Use internal/db.Pool.",    // cure
		"#42",                      // origin
		"go",                       // tags
		"fire-1", "pass-1",         // fixtures
		"fast", // cost
	} {
		if !strings.Contains(out, needed) {
			t.Fatalf("explain output missing %q:\n%s", needed, out)
		}
	}
}

func TestExplainNoFixturesSaysNone(t *testing.T) {
	root := explainRepo(t)
	writeFile(t, root, ".formwork/rules/extra.yaml", `rules:
  - id: zz-no-fixtures
    type: forbidden-pattern
    severity: warn
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'DRAFT'
`)
	code, out, _ := runCLI(t, "explain", "-C", root, "zz-no-fixtures")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "none") {
		t.Fatalf("fixture-less rule must say none:\n%s", out)
	}
}

func TestExplainUnknownRuleExits2NamingId(t *testing.T) {
	root := explainRepo(t)
	code, _, errOut := runCLI(t, "explain", "-C", root, "no-such-rule")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "no-such-rule") {
		t.Fatalf("error must name the unknown id: %s", errOut)
	}
}

func TestExplainJSONRoundTrips(t *testing.T) {
	root := explainRepo(t)
	code, out, _ := runCLI(t, "explain", "-C", root, "-format", "json", "guard-pool")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var got struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Params struct {
			Pattern string `json:"pattern"`
		} `json:"params"`
		Fixtures  []string `json:"fixtures"`
		Allowlist *struct {
			File    string   `json:"file"`
			Entries []string `json:"entries"`
		} `json:"allowlist"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if got.ID != "guard-pool" || got.Params.Pattern != `pgxpool\.New` {
		t.Fatalf("unexpected json: %+v", got)
	}
	if len(got.Fixtures) != 2 || got.Fixtures[0] != "fire-1" || got.Fixtures[1] != "pass-1" {
		t.Fatalf("fixtures: %+v", got.Fixtures)
	}
	if got.Allowlist == nil || got.Allowlist.File != "allowlists/pool.txt" || len(got.Allowlist.Entries) != 2 {
		t.Fatalf("allowlist: %+v", got.Allowlist)
	}
}

func TestIntrospectionEnforcesEngineGate(t *testing.T) {
	// Finding 1: every config-loading command routes through the engine gate;
	// a binary the config refuses must not render guidance (exit 2, naming
	// the engine constraint) — same contract as check/test/lint/scope.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\nengine: \">=99.0.0\"\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: r1
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'x'
`)
	for _, args := range [][]string{
		{"explain", "-C", root, "r1"},
		{"list", "-C", root, "rules"},
		{"list", "-C", root, "lanes"},
		{"rules-for", "-C", root, "a.txt"},
	} {
		code, out, errOut := runCLI(t, args...)
		if code != 2 {
			t.Fatalf("%v: exit %d, want 2 (out: %s)", args, code, out)
		}
		if !strings.Contains(errOut, "engine") {
			t.Fatalf("%v: refusal must name the engine gate: %s", args, errOut)
		}
	}
}

func TestExplainAndListShowLaneAssignment(t *testing.T) {
	// Finding 7: "which lane runs rule X" must be answerable by the binary —
	// the spec's enumeration-replacement claim depends on it. Lane
	// resolution uses the engine's own Selects, never a re-implementation.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
lanes:
  ci:
    all: true
    ci: true
  docs:
    tags: [docs]
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: aa-tagged
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.md"]
    params:
      pattern: 'DRAFT'
    tags: [docs]
  - id: bb-untagged
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.txt"]
    params:
      pattern: 'TODO'
`)
	code, out, _ := runCLI(t, "explain", "-C", root, "aa-tagged")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "lanes: ci, docs") {
		t.Fatalf("explain must show lane assignment:\n%s", out)
	}
	code, jout, _ := runCLI(t, "list", "-C", root, "-format", "json", "rules")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, jout)
	}
	var rows []struct {
		ID    string   `json:"id"`
		Lanes []string `json:"lanes"`
	}
	if err := json.Unmarshal([]byte(jout), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, jout)
	}
	if len(rows) != 2 || len(rows[0].Lanes) != 2 || len(rows[1].Lanes) != 1 || rows[1].Lanes[0] != "ci" {
		t.Fatalf("lane assignment wrong: %+v", rows)
	}
}

func TestExplainJSONFixturesEmptyIsArray(t *testing.T) {
	// Finding 8: empty must render [] like rules-for's rules, never null —
	// the two JSON surfaces must agree on the empty shape.
	root := explainRepo(t)
	writeFile(t, root, ".formwork/rules/extra2.yaml", `rules:
  - id: zz-bare
    type: forbidden-pattern
    severity: warn
    scope:
      include: ["**/*.css"]
    params:
      pattern: 'x'
`)
	code, out, _ := runCLI(t, "explain", "-C", root, "-format", "json", "zz-bare")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if strings.Contains(out, `"fixtures": null`) || !strings.Contains(out, `"fixtures": []`) {
		t.Fatalf("fixtures must render as [], not null:\n%s", out)
	}
}

func TestExplainExceptPathsRenderAsList(t *testing.T) {
	// Finding 9: two carve-out paths must render as one list, not duplicate
	// 'paths:' keys (which strict decode would reject if pasted back).
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: two-except
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'x'
    except:
      paths: ["a.go", "b.go"]
`)
	code, out, _ := runCLI(t, "explain", "-C", root, "two-except")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if got := strings.Count(out, "paths:"); got != 1 {
		t.Fatalf("want exactly one paths: key, got %d:\n%s", got, out)
	}
	for _, needed := range []string{"- a.go", "- b.go"} {
		if !strings.Contains(out, needed) {
			t.Fatalf("missing list entry %q:\n%s", needed, out)
		}
	}
}
