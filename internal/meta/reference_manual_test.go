// reference_manual_test.go — #109's completeness obligation, made executable.
//
// A hand-maintained reference falls behind the engine silently: someone
// registers a rule type, nobody edits the manual, and an adopter reads a
// document that is quietly incomplete. #109's acceptance criteria say
// completeness must be "checked against the registries, not memory" — this is
// that check.
//
// What a "section" means is the whole question. Until #332 it meant the
// backticked NAME appearing anywhere in the file, which is satisfied by a line
// of bare names: the entire Go/Dart/SQL region could be replaced with three
// such lines — every param description, every design note and the Known-limits
// block deleted — and this package stayed green. Meanwhile the manual promises
// its reader that it "cannot fall behind the engine silently". Three
// assertions now stand behind that promise, and each is derived from the
// engine rather than from a list kept here:
//
//	an ENTRY   — an anchored heading or bullet, not a mention in prose
//	its PARAMS — every yaml key the type's factory decodes (reference_source_test.go)
//	an EXAMPLE — a ```yaml block that the type's own factory accepts
//
// The example assertion is the load-bearing one: it is the only one that
// cannot be satisfied by prose. A renamed or removed parameter makes the
// documented example fail to decode, so the manual reddens on an engine change
// rather than after it.
//
// It asserts one direction only: every REGISTERED name has an entry. It
// deliberately does not assert the reverse, because the manual legitimately
// documents things that are not registry entries (the exit-code contract, the
// formwork.yaml schema, the marker grammar), and a two-way check would have to
// enumerate those exceptions — a list that would itself rot.
package meta_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"

	// The registry is populated by whoever imports the rule packages — they
	// self-register in init. Without these, TypeNames() returns only the
	// handful this package's other tests happen to pull in (measured: 4 of
	// 25), and the completeness assertions below pass against a quarter of
	// the vocabulary while looking green. That is the tautological-test shape
	// this repo names explicitly, so the imports are listed here rather than
	// inherited, and minRegisteredTypes stops a future deletion re-hiding it.
	//
	// Mirrors cmd/formwork's set via internal/cli. If a new rule package is
	// added there and not here, minRegisteredTypes is what notices.
	_ "github.com/buildfoundry-nz/formwork/internal/rules/baseline"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/binarycontent"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filesize"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/ordering"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pairconsistency"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
)

// A floor, not a count: the exact number lives in the manual and is asserted
// there. This exists so a dropped blank import cannot make the completeness
// tests vacuous again — which is how they were first written.
const minRegisteredTypes = 20

func referenceManual(t *testing.T) string {
	t.Helper()
	// internal/meta -> repo root
	path := filepath.Join("..", "..", "docs", "reference.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the operator reference is part of the published tree and must exist: %v", err)
	}
	return string(b)
}

func TestReferenceManualCoversEveryRegisteredType(t *testing.T) {
	manual := referenceManual(t)
	names := rules.TypeNames()
	if len(names) < minRegisteredTypes {
		t.Fatalf("only %d rule types registered (want >= %d) — a blank import is "+
			"missing from this file and the assertion below would pass against a "+
			"fraction of the vocabulary", len(names), minRegisteredTypes)
	}
	entries := manualEntries(manual)
	var missing []string
	for _, n := range names {
		// An anchored entry — "#### `name`" or "- **`name`**" at the start of a
		// line — not the name appearing somewhere in 500 lines of prose.
		if _, ok := entries[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("docs/reference.md documents %d of %d registered rule types; missing: %s\n"+
			"A registered type with no entry is a manual that is quietly incomplete — "+
			"add a section rather than deleting this assertion.",
			len(names)-len(missing), len(names), strings.Join(missing, ", "))
	}
}

// An entry that names the type and says nothing else is the shape the old gate
// accepted. A parameter an operator cannot find in the manual is a parameter
// they will not use, or will spell from memory and get exit 2 for; a parameter
// the manual carries that the engine has renamed is worse. Both directions are
// covered here, because the set being compared against is read out of the
// factory's own struct tags rather than kept in a list beside the assertion.
func TestReferenceManualDocumentsEveryParameterOfEveryType(t *testing.T) {
	manual := referenceManual(t)
	entries := manualEntries(manual)
	params := registeredParams(t)
	for _, n := range rules.TypeNames() {
		entry, ok := entries[n]
		if !ok {
			continue // already reported, in full, by the completeness gate
		}
		ps := params[n]
		if !ps.resolved {
			continue // already reported by TestReferenceParamsResolveForEveryRegisteredType
		}
		if len(ps.names) == 0 {
			// Silence is not documentation: "this takes no parameters" is a
			// fact the reader needs, and it is also what stops an entry
			// passing this gate because the extractor came back empty.
			if !strings.Contains(entry, "no parameters") {
				t.Errorf("%s decodes no parameters, and its entry must say so in those words — "+
					"an entry that simply omits them reads as an entry that forgot them", n)
			}
			continue
		}
		var missing []string
		for _, p := range ps.names {
			if !strings.Contains(entry, "`"+p+"`") {
				missing = append(missing, p)
			}
		}
		if len(missing) > 0 {
			t.Errorf("the %s entry does not document %d of its %d decoded parameters: %s\n"+
				"These are read from the struct the factory decodes into, so this is the "+
				"manual falling behind the engine — the failure this file exists to prevent.",
				n, len(missing), len(ps.names), strings.Join(missing, ", "))
		}
	}
}

// #109 asked for "a minimal example" per type and the manual shipped none: no
// fenced block appears anywhere in its Rule types section. Prose examples rot
// invisibly, so the assertion is not that an example exists but that it WORKS:
// each example is written out as a rule file and LOADED — the same strict
// decode, the same registry lookup, the same factory an operator's own config
// goes through.
//
// Loading, rather than handing the params node to the factory, because a rule
// stanza is more than its params. Measured while writing the examples: a
// factory-level check accepts
//
//   - id: dart-analyzes-clean
//     type: command
//     cost: heavy          # ruleSpec has no `cost` field
//
// because it never sees the key, while `formwork check` on that file is
// "field cost not found in type config.ruleSpec" at exit 2. An example that
// fails for the reader is worse than no example, so the gate has to run the
// reader's path.
func TestReferenceManualGivesEveryTypeAWorkingExample(t *testing.T) {
	manual := referenceManual(t)
	entries := manualEntries(manual)
	for _, n := range rules.TypeNames() {
		entry, ok := entries[n]
		if !ok {
			continue // already reported by the completeness gate
		}
		blocks := fencedBlocks(entry, "yaml")
		if len(blocks) == 0 {
			t.Errorf("the %s entry carries no ```yaml example — a reference that never shows "+
				"the type configured leaves its parameter list to be guessed at", n)
			continue
		}
		shown := false
		for _, b := range blocks {
			declares, err := exampleDeclares(b, n)
			if err != nil {
				t.Errorf("a ```yaml block in the %s entry is not valid YAML: %v\n%s", n, err, b)
				continue
			}
			if !declares {
				continue
			}
			shown = true
			cfg, err := loadExample(t, b)
			if err != nil {
				t.Errorf("the %s example is not a configuration the engine accepts: %v\n%s", n, err, b)
				continue
			}
			if !carriesRuleOfType(cfg, n) {
				t.Errorf("the %s example loaded, but produced no rule of that type", n)
			}
		}
		if !shown {
			t.Errorf("no ```yaml block in the %s entry declares `type: %s`, so nothing ties the "+
				"example to the rule type it is meant to demonstrate", n, n)
		}
	}
}

// loadExample writes one example into a throwaway repo root and loads it the
// way the binary loads a real one. The example may be written as a bare
// sequence of rules or with its own `rules:` key — whichever reads best at
// that point in the manual — so the wrapper is added only when it is missing.
func loadExample(t *testing.T, block string) (*config.Config, error) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".formwork", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("cannot build a throwaway config root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".formwork", "formwork.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("cannot write the throwaway envelope: %v", err)
	}
	doc := block
	if !strings.HasPrefix(strings.TrimSpace(doc), "rules:") {
		doc = "rules:\n" + doc
	}
	if err := os.WriteFile(filepath.Join(dir, "example.yaml"), []byte(doc+"\n"), 0o644); err != nil {
		t.Fatalf("cannot write the example rule file: %v", err)
	}
	return config.Load(root)
}

func carriesRuleOfType(cfg *config.Config, typ string) bool {
	for _, r := range cfg.Rules {
		if r.Type == typ {
			return true
		}
	}
	return false
}

// exampleDeclares reports whether a fenced block configures typ. The walk is
// over the whole document rather than an assumed shape, so the manual chooses
// how to write an example and the gate reads it either way.
func exampleDeclares(block, typ string) (declares bool, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		return false, err
	}
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil || declares {
			return
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if k, v := n.Content[i], n.Content[i+1]; k.Value == "type" && v.Value == typ {
					declares = true
					return
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	return declares, nil
}

func TestReferenceManualCoversEveryRegisteredPreprocessor(t *testing.T) {
	manual := referenceManual(t)
	names := preprocess.Names()
	if len(names) == 0 {
		t.Fatal("no preprocessors registered — this test would pass vacuously")
	}
	var missing []string
	for _, n := range names {
		if !strings.Contains(manual, "`"+n+"`") {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("docs/reference.md documents %d of %d registered preprocessors; missing: %s",
			len(names)-len(missing), len(names), strings.Join(missing, ", "))
	}
}

// The counts stated in the manual's prose are a claim a reader will trust
// without running anything, so they are checked too. This is the assertion that
// fires first when someone registers a type: the section may be missing AND the
// count stale, and the count is the cheaper thing to notice.
//
// EVERY count, not one. Checking that the live count appears SOMEWHERE let a
// second, stale statement of the same count ship and stay green: sql/locking-
// target was registered in 28ddc1f0, the header line at the top of the file was
// updated to 26 and "25 registered." at the head of the Rule types section was
// not. Both are the same claim to a reader. So the manual is scanned for the
// shape of a count claim and every one of them is compared with the registry.
//
// A count that names no registry — "25 registered." — is refused outright, for
// the same reason: nothing can check it, which is exactly how that one outlived
// the type it was wrong about.
var (
	countClaimRe = regexp.MustCompile(`(\d+) (?:registered )?(rule types|preprocessors)`)
	bareCountRe  = regexp.MustCompile(`(\d+) registered(?: (rule types|preprocessors))?`)
)

func TestReferenceManualStatesTheRegistryCountsCorrectly(t *testing.T) {
	manual := referenceManual(t)
	live := map[string]int{
		"rule types":    len(rules.TypeNames()),
		"preprocessors": len(preprocess.Names()),
	}
	for what, n := range live {
		// Stated at least once, so the sweep below cannot pass by finding
		// nothing to check.
		want := strconv.Itoa(n) + " " + what
		if !strings.Contains(manual, want) {
			t.Errorf("docs/reference.md must state the live count %q; it does not", want)
		}
	}
	for _, m := range countClaimRe.FindAllStringSubmatch(manual, -1) {
		stated, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if stated != live[m[2]] {
			t.Errorf("docs/reference.md says %q; the registry has %d %s. Every count in this "+
				"file is a claim the reader cannot check without a binary — a second, stale "+
				"one is how \"25 registered.\" outlived the 26th type.", m[0], live[m[2]], m[2])
		}
	}
	for _, m := range bareCountRe.FindAllStringSubmatch(manual, -1) {
		if m[2] == "" {
			t.Errorf("docs/reference.md says %q, which names no registry — so nothing can "+
				"check it, and it goes stale silently. Write %q or %q instead.",
				m[0], m[1]+" registered rule types", m[1]+" registered preprocessors")
		}
	}
}

// #99's resolution belongs where an operator will meet it. The spec explains
// that --range is one string tokenized shell-style and that a pathspec
// containing a space must therefore be quoted; the reference said only that
// --range "takes an optional `-- <pathspec>` tail". Unquoted, that pathspec
// splits in two, git matches nothing and exits 0, and every per-file rule sees
// an empty changeset — the gate passes over unscanned work. Someone reading the
// reference alone could not know to quote it.
func TestReferenceManualDocumentsRangeTokenization(t *testing.T) {
	section := manualSection(t, referenceManual(t), "### File-set modes")
	for _, want := range []struct{ text, why string }{
		{"shell-style", "`--range` is ONE string split on unquoted whitespace, not a list of arguments"},
		{"quot", "quoting is the whole resolution of #99 — a spaced pathspec is unrepresentable without it"},
		{"exit 2", "an unclosed quote or dangling escape is an error, not a guess at what was meant"},
	} {
		if !strings.Contains(section, want.text) {
			t.Errorf("the File-set modes section must say %q — %s\nsection:\n%s", want.text, want.why, section)
		}
	}
}

// The document's own account of what checks it, checked.
//
// "How completeness is checked" is the section a reader trusts INSTEAD of
// reading this file, and it was the strongest over-claim in the manual: it
// promised that a gate naming one test meant "this document cannot fall behind
// the engine silently", while that test compared names and nothing else. A
// promise about a gate is worth exactly what the gate does, so the section must
// name every assertion standing behind it — and every test it names must exist.
//
// The names are taken from the functions themselves rather than written out as
// strings, so renaming one updates what this test demands and deleting one is a
// compile error, not a doc that quietly describes a test nobody runs.
func testFuncName(fn func(*testing.T)) string {
	full := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	if i := strings.LastIndex(full, "."); i >= 0 {
		return full[i+1:]
	}
	return full
}

var declaredTestRe = regexp.MustCompile(`func (Test\w+)\(`)

func TestReferenceManualDescribesTheGateItActuallyHas(t *testing.T) {
	section := manualSection(t, referenceManual(t), "## How completeness is checked")
	for _, fn := range []func(*testing.T){
		TestReferenceManualCoversEveryRegisteredType,
		TestReferenceManualCoversEveryRegisteredPreprocessor,
		TestReferenceManualDocumentsEveryParameterOfEveryType,
		TestReferenceManualGivesEveryTypeAWorkingExample,
		TestReferenceManualStatesTheRegistryCountsCorrectly,
	} {
		name := testFuncName(fn)
		if !strings.Contains(section, "`"+name+"`") {
			t.Errorf("\"How completeness is checked\" does not name %s, so the promise it makes "+
				"to the reader is broader than the gate it points at — which is the defect this "+
				"assertion exists to stop", name)
		}
	}
	// The other direction: a named test that no longer exists is the same lie
	// told the other way round.
	declared := map[string]bool{}
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("cannot read internal/meta: %v", err)
	}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("cannot read %s: %v", e.Name(), err)
		}
		for _, m := range declaredTestRe.FindAllStringSubmatch(string(b), -1) {
			declared[m[1]] = true
		}
	}
	if len(declared) == 0 {
		t.Fatal("no test functions found in internal/meta — the check below would pass vacuously")
	}
	for _, m := range regexp.MustCompile("`(Test\\w+)`").FindAllStringSubmatch(section, -1) {
		if !declared[m[1]] {
			t.Errorf("the section names %s, which no test in internal/meta declares — a document "+
				"citing a gate that does not exist is worse than one citing none", m[1])
		}
	}
}

// #294 — the sql/locking-target entry must disclose what its parameters ARE.
//
// `table` is an unanchored RE2 regex matched against the relation NAME alone.
// The manual called it "which relation a locking clause locks", which reads as
// a name, so the natural spelling `table: <schema>.<table>` — how anyone who
// has written SQL names a relation — matched nothing. A sibling entry in
// the same section, sql/statement-predicate, gives an identically named param
// incompatible semantics (it matches the statement TEXT, where the qualified
// spelling does work), which is what makes the wrong mental model the natural
// one. That is a documentation defect, not only a code one: the decision was
// recorded in a PR body and a Go comment, where no operator reads it.
//
// Asserted against the entry, not the whole manual, so the words cannot be
// satisfied by some unrelated section that happens to use them.
func manualEntry(t *testing.T, manual, typ string) string {
	t.Helper()
	// Entry boundaries are defined once, in reference_source_test.go, so an
	// assertion about an entry and the completeness gate over the same entry
	// cannot disagree about where it ends.
	entry, ok := manualEntries(manual)[typ]
	if !ok {
		t.Fatalf("docs/reference.md has no entry for %s", typ)
	}
	return entry
}

func TestReferenceManualDisclosesLockingTargetParameterSemantics(t *testing.T) {
	entry := manualEntry(t, referenceManual(t), "sql/locking-target")
	for _, want := range []struct{ text, why string }{
		{"unanchored", "`table` is an unanchored regex: `project` matches the relation `projects`"},
		{"RE2", "`table` is a regex, not a name — the single most load-bearing fact about it"},
		{"`schema`", "the parameter that expresses schema qualification must be documented"},
		{"guards every schema", "`schema` is OPTIONAL, and what an absent one does is the fact that decides " +
			"whether the rule guards everything or one thing. An operator who reads it as defaulting to " +
			"`public` writes a rule they believe is narrow and is not — the same wrong-mental-model trap " +
			"the `table` facts above exist for, one param over"},
		{"search_path", "a relation the source does not qualify is reported, because its schema is a run-time search_path decision"},
	} {
		if !strings.Contains(entry, want.text) {
			t.Errorf("the sql/locking-target entry must say %q — %s\nentry:\n%s", want.text, want.why, entry)
		}
	}
}

// The sibling half of the same trap. sql/statement-predicate's `table` is an
// unanchored RE2 regex matched against the whole STATEMENT TEXT
// (internal/rules/sqltext/sqltext.go — c.table.MatchString(s.text)), so
// `app\.projects` does work there. Same param name, incompatible semantics, one
// section apart; documenting only the locking-target half would leave the
// contrast asserted from one side.
func TestReferenceManualDisclosesStatementPredicateTableSemantics(t *testing.T) {
	entry := manualEntry(t, referenceManual(t), "sql/statement-predicate")
	for _, want := range []struct{ text, why string }{
		{"unanchored", "`table` is unanchored here too"},
		{"RE2", "`table` is a regex, not a name"},
		{"statement text", "it is matched against the whole statement, which is what makes it accept a schema-qualified spelling when sql/locking-target's does not"},
	} {
		if !strings.Contains(entry, want.text) {
			t.Errorf("the sql/statement-predicate entry must say %q — %s\nentry:\n%s", want.text, want.why, entry)
		}
	}
}
