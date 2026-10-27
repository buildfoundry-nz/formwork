// Package vacuity judges whether a PASS fixture could ever have failed its
// rule. A pass fixture is green by definition — no finding — so green alone
// proves nothing: a fixture the rule never reaches, or one that never contains
// the subject a name-anchored rule watches, is green under any pattern and any
// scope. Two verdicts, both fail-by-default in `formwork test`:
//
//   - PASS-FIXTURE-VACUOUS: the rule reaches no file of the fixture AND would
//     have judged nothing in it even with a scope that subtracts nothing.
//   - PASS-FIXTURE-BLIND: the rule reaches the fixture but the fixture holds no
//     occurrence of the symbol/sink the rule is anchored on (go/call-confined-
//     to-func-name, go/guard-precedes-call only).
//
// Rules whose verdict does not come from a cheap in-process pass over the tree
// (anything not CostFast: command, git-diff, ...) are "not-judged" — reported,
// never failed, never silent.
package vacuity

import (
	"fmt"
	"regexp"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// Verdict kinds.
const (
	KindJudged    = "judged"
	KindVacuous   = "vacuous"
	KindBlind     = "blind"
	KindNotJudged = "not-judged"
)

// Verdict codes, carried only by the two failing kinds.
const (
	CodeVacuous = "PASS-FIXTURE-VACUOUS"
	CodeBlind   = "PASS-FIXTURE-BLIND"
)

// Verdict is the judgement of one pass fixture.
type Verdict struct {
	Kind   string // KindJudged | KindVacuous | KindBlind | KindNotJudged
	Code   string // "" | CodeVacuous | CodeBlind
	Reason string // set for every kind except judged
}

// NotJudgedEscape is the verdict for a rule outside the verdicts' reach. It is
// exported so a caller that already knows a rule is an escape (the compile-once
// command path) can report it without a probe.
func NotJudgedEscape(r *config.Rule) Verdict {
	return Verdict{
		Kind: KindNotJudged,
		Reason: fmt.Sprintf("%s rule: its verdict comes from outside a cheap in-process pass, so no vacuity probe applies",
			r.Type),
	}
}

// IsEscape reports whether r is outside the verdicts' reach: any cost class
// other than fast.
func IsEscape(r *config.Rule) bool {
	return rules.CostOf(r.Checker) != rules.CostFast
}

// Judge judges one pass fixture, fs being the fixture's already-walked tree.
// An escape rule is not-judged. Otherwise vacuous is checked first, then blind;
// a rule that is neither is judged. A probe that cannot run is an error — the
// caller must exit 2, never read it as a pass.
//
// inert carries the except.paths entries their authors declared inert (see
// DeclaredInert); a fixture standing at one is not vacuous. nil excuses nothing.
func Judge(r *config.Rule, fs *scan.FileSet, inert DeclaredInert) (Verdict, error) {
	return judgeWith(r, fs, inert, AnchorParams)
}

func judgeWith(r *config.Rule, fs *scan.FileSet, inert DeclaredInert, anchor func(*config.Rule) (AnchorParamSet, error)) (Verdict, error) {
	if IsEscape(r) {
		return NotJudgedEscape(r), nil
	}
	vac, err := Vacuous(r, fs, func(fs *scan.FileSet) bool { return inert.Excuses(r.ID, fs) })
	if err != nil {
		return Verdict{}, err
	}
	if vac {
		return Verdict{
			Kind: KindVacuous, Code: CodeVacuous,
			Reason: "the clean fixture has no file this rule reaches and nothing this rule would have judged — " +
				"it is green under any pattern and any scope, so it demonstrates nothing",
		}, nil
	}
	return blind(r, fs, anchor)
}

// Vacuous reports whether a clean fixture could never have been judged by r:
// the rule reaches none of its files AND, with a scope subtracting nothing, the
// engine finds nothing to say about it. "No in-scope file" alone is not the
// offence — parking a real violation at an excluded path is how an exclusion
// glob is held in place — so the second half is decided by engine.Run over a
// universal-scope twin, which gets polarity right (a ban is violated by a match,
// a required-pattern by an absence) without re-implementing any matcher.
//
// declaredInert, when non-nil, lets the caller excuse a fixture standing at a
// path its author declared inert (Judge passes DeclaredInert.Excuses).
//
// Heavy rules are out of class: their tool runs at the tree root and never
// consults scope for its verdict.
func Vacuous(r *config.Rule, fs *scan.FileSet, declaredInert func(*scan.FileSet) bool) (bool, error) {
	if rules.CostOf(r.Checker) == rules.CostHeavy {
		return false, nil
	}
	if reachesAnyFile(r, fs) {
		return false, nil
	}
	if declaredInert != nil && declaredInert(fs) {
		return false, nil
	}
	judged, err := judgesAnything(r, fs)
	if err != nil {
		return false, fmt.Errorf("rule %s: universal-scope probe: %w", r.ID, err)
	}
	return !judged, nil
}

func reachesAnyFile(r *config.Rule, fs *scan.FileSet) bool {
	for _, f := range fs.Files {
		if r.Applies(f.Path()) {
			return true
		}
	}
	return false
}

// judgesAnything reports whether r finds anything to say about fs once its
// scope subtracts nothing. A Fresh checker, because a rule carries per-run
// state (required-pattern exists mode accumulates across files). Preprocess and
// Marker are carried across because they change what the checker SEES.
func judgesAnything(r *config.Rule, fs *scan.FileSet) (bool, error) {
	fresh, err := r.Fresh()
	if err != nil {
		return false, err
	}
	probe, err := config.New(r.ID, r.Type, r.Severity, r.Cure, []string{"**"}, nil, nil, fresh.Checker)
	if err != nil {
		return false, err
	}
	probe.Preprocess = r.Preprocess
	probe.Marker = r.Marker
	fds, err := engine.Run([]*config.Rule{probe}, fs, 1)
	if err != nil {
		return false, err
	}
	return len(finding.Unsuppressed(fds)) > 0, nil
}

// NeverMatches compiles and can never match: end-of-text followed by
// start-of-text. Substituted for the half of a rule that EXCUSES an occurrence
// (allowed_func, guard), it turns the rule into "flag every occurrence of the
// anchor".
const NeverMatches = `$^`

// probeIdent is a stand-in func name used to tell "admits nothing" from "admits
// something real" in totalBanConfinement.
const probeIdent = "SomeFuncName"

// blind answers, of a clean fixture, whether it holds any occurrence of the
// subject r is anchored on. Only go/call-confined-to-func-name and
// go/guard-precedes-call are name-anchored this way; any other type is judged.
//
// Two quirks are kept deliberately, as ported:
//   - vacuous counts only UNSUPPRESSED findings (judgesAnything), while blind's
//     AnchorHits counts suppressed ones too — a marker-suppressed symbol proves
//     the anchor still sees its subject.
//   - a guard-precedes-call rule with `funcs` set runs a SECOND probe with the
//     funcs filter defeated as well, because a pass fixture can legitimately
//     assert that the filter excludes an unguarded sink.
//
// Silent exits of the port, and what each now means: a total ban, a fixture the
// rule reaches no file of, hits>0 and a funcs-probe hit are all KindJudged (the
// fixture is fine, or the question belongs to vacuous). An anchor-params error
// or an empty anchor pattern is a config defect and returns an error.
func blind(r *config.Rule, fs *scan.FileSet, anchor func(*config.Rule) (AnchorParamSet, error)) (Verdict, error) {
	var (
		override  map[string]string
		field, pt string
	)
	switch r.Type {
	case "go/call-confined-to-func-name", "go/guard-precedes-call":
	default:
		return Verdict{Kind: KindJudged}, nil
	}
	p, err := anchor(r)
	if err != nil {
		return Verdict{}, fmt.Errorf("rule %s: reading anchor params: %w", r.ID, err)
	}
	if r.Type == "go/call-confined-to-func-name" {
		if totalBanConfinement(p.AllowedFunc) {
			return Verdict{Kind: KindJudged}, nil // absence IS this rule's compliant end state
		}
		override, field, pt = map[string]string{"allowed_func": NeverMatches}, "params.symbol", p.Symbol
	} else {
		override, field, pt = map[string]string{"guard": NeverMatches}, "params.sink", p.Sink
	}
	if pt == "" {
		return Verdict{}, fmt.Errorf("rule %s: %s is empty, so the anchor probe has nothing to look for", r.ID, field)
	}
	if !reachesAnyFile(r, fs) {
		return Verdict{Kind: KindJudged}, nil // vacuous's question, decided before blind is reached
	}
	hits, err := AnchorHits(r, fs, override)
	if err != nil {
		return Verdict{}, fmt.Errorf("rule %s: anchor probe: %w", r.ID, err)
	}
	if hits > 0 {
		return Verdict{Kind: KindJudged}, nil
	}
	if p.Funcs != "" {
		wider, err := AnchorHits(r, fs, map[string]string{"guard": NeverMatches, "funcs": "."})
		if err != nil {
			return Verdict{}, fmt.Errorf("rule %s: funcs-defeated anchor probe: %w", r.ID, err)
		}
		if wider > 0 {
			return Verdict{Kind: KindJudged}, nil // it holds the funcs filter in place
		}
	}
	return Verdict{
		Kind: KindBlind, Code: CodeBlind,
		Reason: "the clean fixture holds no occurrence of " + field + " (" + pt + ") even with the rule's " +
			"exempting half defeated, so it is green under any pattern and any re-anchoring — it demonstrates " +
			"nothing about the invariant. A pass fixture must show the subject PRESENT and correctly placed",
	}, nil
}

// AnchorParamSet is the subset of goast params the blind probe anchors on.
type AnchorParamSet struct {
	AllowedFunc string `yaml:"allowed_func"`
	Symbol      string `yaml:"symbol"`
	Sink        string `yaml:"sink"`
	Funcs       string `yaml:"funcs"`
}

// AnchorParams reads the name-anchoring params off the rule as written. Only
// metadata is read here; every verdict is still rendered by the engine.
func AnchorParams(r *config.Rule) (AnchorParamSet, error) {
	var p AnchorParamSet
	raw, err := r.ParamsYAML()
	if err != nil || raw == "" {
		return p, err
	}
	if err := yaml.Unmarshal([]byte(raw), &p); err != nil {
		return p, err
	}
	return p, nil
}

// totalBanConfinement reports whether allowed_func declares a TOTAL BAN: it
// admits the empty name and no real one, so the compliant end state is the
// symbol matching nothing. `^$` and `$^` say "no sanctioned caller"; `.*`
// admits real names (an exemption, not a ban); a real name is an ordinary
// confinement over a symbol expected to be live.
func totalBanConfinement(allowedFunc string) bool {
	if allowedFunc == "" {
		return false
	}
	re, err := regexp.Compile(allowedFunc)
	if err != nil {
		return false
	}
	return re.MatchString("") && !re.MatchString(probeIdent)
}

// AnchorHits recompiles r with override applied to its params and returns how
// many findings — suppressed ones included — the real engine produces over fs.
// scope.exclude and except.paths are left as written: a symbol surviving only
// in a path the rule excludes is invisible TO THE RULE.
func AnchorHits(r *config.Rule, fs *scan.FileSet, override map[string]string) (int, error) {
	checker, err := recompileWith(r, override)
	if err != nil {
		return 0, err
	}
	probe := r.CloneWithChecker(checker)
	fds, err := engine.Run([]*config.Rule{probe}, fs, 1)
	if err != nil {
		return 0, err
	}
	return len(fds), nil
}

// recompileWith builds a checker for r's type from r's own params with override
// applied, through the registry's factory, so the variant is compiled by the
// same code that compiled the rule.
func recompileWith(r *config.Rule, override map[string]string) (rules.Checker, error) {
	raw, err := r.ParamsYAML()
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("rule %s: params did not render as a document", r.ID)
	}
	params := doc.Content[0]
	if params.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("rule %s: params are not a mapping", r.ID)
	}
	for key, val := range override {
		setScalar(params, key, val)
	}
	factory, ok := rules.Lookup(r.Type)
	if !ok {
		return nil, fmt.Errorf("rule %s: no factory registered for type %q", r.ID, r.Type)
	}
	return factory(params)
}

// setScalar sets key to val on a mapping node, appending the pair when the key
// is absent so an override never silently does nothing.
func setScalar(m *yaml.Node, key, val string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Kind = yaml.ScalarNode
			m.Content[i+1].Tag = "!!str"
			m.Content[i+1].Style = yaml.DoubleQuotedStyle
			m.Content[i+1].Value = val
			m.Content[i+1].Content = nil
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: val},
	)
}
