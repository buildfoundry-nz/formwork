package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// arm is one rule arm as DECLARED, read straight off the YAML rather than off
// the compiled rule: config.Rule keeps the compiled checker and throws the
// params away, and the questions this census asks are about what the author
// wrote (the unit and the obligation), not about the engine's resolution of
// them.
type arm struct {
	File string // repo-relative rule file
	Line int    // 1-based line the arm's mapping opens on
	ID   string
	Type string

	Trigger    string
	Where      string
	Obligation string
}

// armSpec is the decode target. Unknown params keys are ignored, so every rule
// type in the corpus decodes — a `type: command` arm simply yields empty
// trigger/where/obligation, which is exactly what "not a pair arm" should look
// like.
type armSpec struct {
	ID     string `yaml:"id"`
	Type   string `yaml:"type"`
	Params struct {
		Trigger    string `yaml:"trigger"`
		Where      string `yaml:"where"`
		Obligation string `yaml:"obligation"`
	} `yaml:"params"`
}

// loadCorpus reads every arm in root/.formwork/rules/*.yaml.
//
// Everything — params AND line numbers — comes from ONE yaml.Node decode. A
// pattern is an arbitrary quoted string that routinely contains `#`, `:` and
// backslash escapes, so reading params off a line regex is a false verdict in
// either direction; and reading the LINE off a `^\s*- id:` text scan assumes
// `id` is the arm's first key, which does not hold in the one place the census
// is most load-bearing: mutation-proof re-marshals each rule into a scratch
// corpus (scripts/dev/mutation-proof/prove.go), and yaml.Marshal of a map
// emits keys ALPHABETICALLY. The node's own Line is the same fact without the
// assumption. (Same idiom as tools/formwork-arm-census/corpus.go.)
func loadCorpus(root string) ([]arm, error) {
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, err
	}
	// A run that read no rule FILE at all is a broken invocation wearing a
	// pass: a wrong root or a moved rules directory would otherwise report
	// "0 flagged", which is indistinguishable from a clean corpus. An empty
	// SUBJECT — rule files, no judged arms — is a legitimate pass, and is the
	// shape the mutation-proof scratch hands this census.
	if len(files) == 0 {
		return nil, fmt.Errorf("no rule files found under %s — refusing to report a pass", filepath.Join(root, ".formwork", "rules"))
	}
	sort.Strings(files)
	var out []arm
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		rel := filepath.ToSlash(relTo(root, f))
		arms, err := armsIn(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		for i := range arms {
			arms[i].File = rel
		}
		out = append(out, arms...)
	}
	return out, nil
}

// armsIn decodes one rule file into its arms, in declaration order. A file
// whose top level is not a mapping with a `rules` sequence is an ERROR rather
// than an empty result: a rule file the census silently read as "no arms" is a
// rule file the census is not covering, which is the defect it exists to find.
func armsIn(data []byte) ([]arm, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("no YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("top level is not a mapping")
	}
	var rules *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "rules" {
			rules = root.Content[i+1]
			break
		}
	}
	if rules == nil {
		return nil, fmt.Errorf("no rules key")
	}
	if rules.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("rules is not a sequence")
	}
	out := make([]arm, 0, len(rules.Content))
	for _, n := range rules.Content {
		var s armSpec
		if err := n.Decode(&s); err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		out = append(out, arm{
			Line: n.Line, ID: s.ID, Type: s.Type,
			Trigger: s.Params.Trigger, Where: s.Params.Where, Obligation: s.Params.Obligation,
		})
	}
	return out, nil
}

func relTo(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

// isJudgedArm is the census's SUBJECT TEST, deliberately one predicate so a
// mutation proof can invert it in one edit
// (.formwork/mutations/pair-consistency-not-for-countable-obligations.yaml).
// Judged: a pair-consistency arm that OMITTED `where:` (the engine's default
// is same-file, pinned upstream by TestPairConsistencyDefaultWhereIsSameFile)
// with the default presence obligation. That omitted default is the trap
// (#12181). An explicit `where: same-file` is a design act — explain-gate
// argues for file grain because ANALYZE and GENERIC_PLAN cannot share a
// function, and same-dir would let one EXPLAIN gate buy another in the same
// package — the same class as same-dir, not a forgotten conversion. A
// same-func arm owes a companion per function and a countable arm owes
// count(requires) >= count(trigger) per unit, so both are per-occurrence by
// construction.
func isJudgedArm(a arm) bool {
	if a.Type != "pair-consistency" {
		return false
	}
	if a.Where != "" {
		return false
	}
	return a.Obligation != "countable"
}

// countBlind is the single verdict primitive detect() and the calibration
// share: more than one trigger line in one JUDGED file. The domain test is
// per file — "the offending file has a per-unit predicate" — .go, .dart and
// .proto, the three languages pair-consistency same-func can unit. Shell
// stays out: there is no .sh unit, and converting go-compile-gate-disk-probe
// would take it from count-blind to totally blind (#12195 work item 4). The
// first draft keyed the domain on the arm's glob list (hasGoInScope), which
// admitted mixed-scope arms whose evidence is entirely non-Go.
func countBlind(n int, path string) bool {
	if n <= 1 {
		return false
	}
	return strings.HasSuffix(path, ".go") ||
		strings.HasSuffix(path, ".dart") ||
		strings.HasSuffix(path, ".proto")
}

// detect flags every judged arm with an in-scope file that carries more than
// one trigger line.
//
// The scope and the preprocessing come from the COMPILED rule — config.Load +
// scan.Walk + Rule.Applies + File.Variant — so the count is the one the gate
// itself would compute for the same arm, never a hand-rolled approximation.
func detect(root string, arms []arm) ([]offender, int, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*config.Rule, len(cfg.Rules))
	for _, r := range cfg.Rules {
		byID[r.ID] = r
	}
	fset, err := scan.Walk(root)
	if err != nil {
		return nil, 0, err
	}
	var bad []offender
	examined := 0
	for _, a := range arms {
		if !isJudgedArm(a) {
			continue
		}
		r, ok := byID[a.ID]
		if !ok {
			// The corpus reader saw an arm the engine did not load. That is a
			// loader disagreement, not a clean skip: reporting it as "nothing
			// to measure" is how a rule leaves a census unnoticed.
			return nil, 0, fmt.Errorf("%s:%d: arm %q is declared but the engine did not load it", a.File, a.Line, a.ID)
		}
		examined++
		// The engine compiles pair-consistency patterns with regexp.Compile
		// (RE2) — there is no syntax key on this rule type — so the same
		// compiler here keeps a census count and a gate count identical.
		re, err := regexp.Compile(a.Trigger)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d (%s): invalid trigger: %w", a.File, a.Line, a.ID, err)
		}
		for _, f := range fset.Files {
			if !r.Applies(f.Path()) {
				continue
			}
			v, err := f.Variant(r.Preprocess)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
			}
			lines, err := v.Lines()
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
			}
			n := 0
			for _, l := range lines {
				if re.MatchString(l) {
					n++
				}
			}
			if countBlind(n, f.Path()) {
				bad = append(bad, offender{a.File, a.Line, a.ID, fmt.Sprintf(
					"%s carries %d trigger lines, and one companion clears them all at file grain. "+
						"Convert to `where: same-func` so each function owes its own companion, to "+
						"`obligation: countable` where the counts genuinely couple per file, or declare "+
						"`where: same-file` when file grain is the signed unit.", f.Path(), n)})
			}
		}
	}
	return bad, examined, nil
}
