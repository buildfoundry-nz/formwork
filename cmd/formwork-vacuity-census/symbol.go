// Symbol-anchor probes. A rule that selects its subject by NAME — the `symbol`
// of a call-confined-to-func-name confinement, the `sink` a guard-precedes-call
// rule guards, the `funcs` filter either of them admits through — stops seeing
// anything the moment that name is deleted or renamed, and then reports green
// forever.
//
// The engine already names this failure and already owns the mechanism:
// rules.AnchorProbe is wired into call-order-in-func, per-func-count-relation
// and the Dart method-delegates type. It is NOT wired into
// call-confined-to-func-name or guard-precedes-call, which is 41 live rules
// across the two types. These arms are the census spelling of that probe for
// the two types the engine skips, exactly as DEAD-TRIGGER is its
// pair-consistency spelling (#12181).
//
// WHAT "DEAD" MEANS HERE, and why the obvious reading is wrong. A confinement
// is a BAN. Zero call sites today does not make a ban unfallible — if the
// symbol still EXISTS, a new call site can appear tomorrow and the ban catches
// it. Read naively, "no call site" condemns a rule for being OBEYED. Measured
// against the real 1873-rule corpus, that naive reading produced five findings
// and all five were false: .GenerateContent (SDK, used as a method value rather
// than a call), json.Marshal (stdlib, in a file that moved to proto.Marshal),
// ReducePhases, FindRole and sectionsComplete (all still declared). The last is
// the sharpest — its rule is a TOTAL ban that carves its legitimate callers out
// through scope.exclude, so zero call sites in scope is precisely its compliant
// state.
//
// So the verdict needs BOTH halves #12494 asked for: no call site AND no
// declaration. Only a symbol that exists nowhere can never be called.
//
// The total-ban case needs no branch of its own: a total ban whose symbol is
// declared is silenced by the declaration test, and one whose symbol is
// genuinely gone IS vacuous and should be reported like any other.
package main

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// neverMatches compiles and can never match: end-of-text followed by
// start-of-text. Substituting it for the half of a rule that EXCUSES an
// occurrence — allowed_func on a confinement, guard on a guard-precedes —
// turns that rule into "flag every occurrence of the anchor".
const neverMatches = `$^`

// goFuncDecl matches a Go function or method declaration and captures its name.
// A line scan rather than a parse: this runs over every Go file in the tree, and
// the question — "does a func by this name exist anywhere" — does not need an
// AST. Over-matching is harmless here, because every extra name can only
// SILENCE a verdict, never manufacture one.
var goFuncDecl = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)

// declName returns the function or method name a line declares, or "".
//
// The prefix test is not a micro-optimisation looking for a home: the regex is
// anchored at ^func, so the guard is exact, and it keeps the regex engine off
// the ~97% of lines that cannot match. Measured over this tree's 9947 Go files
// and 1597435 lines: 475ms unguarded, 233ms guarded, identical 41933 names. The
// pass runs on the Architecture Guardrails path, so the cheap half is worth
// taking; the file reads themselves are already cached by scan.File for the
// whole census and are not re-paid here.
func declName(line string) string {
	if !strings.HasPrefix(line, "func") {
		return ""
	}
	if m := goFuncDecl.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// declaredFuncNames collects every function and method name declared anywhere in
// the scanned tree.
//
// Whole-tree rather than in-scope on purpose: the question is whether the symbol
// EXISTS, not whether the rule can currently see it. A reader confined to one
// package is still callable from the package that declares it.
func declaredFuncNames(fset *scan.FileSet) map[string]bool {
	out := map[string]bool{}
	for _, f := range fset.Files {
		if !strings.HasSuffix(f.Path(), ".go") {
			continue
		}
		lines, err := f.Lines()
		if err != nil {
			continue
		}
		for _, ln := range lines {
			if n := declName(ln); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

// qualifiedPattern reports whether the symbol pattern names something through a
// package or receiver selector — json.Marshal, pipelinephases.ReducePhases, a
// bare .GenerateContent.
//
// Such a subject is declared in ANOTHER package, frequently outside this tree
// entirely (stdlib, a module dependency). "No declaration here" therefore proves
// nothing about whether it can be called, and the census has no type information
// to resolve it with, so it must not decide. Any dot counts, including one
// inside an alternation like (^|\.)Name$, which matches both spellings — the
// conservative reading, since the cost of over-including is one unreported rule
// and the cost of under-including is condemning a live gate.
func qualifiedPattern(pat string) bool { return strings.Contains(pat, ".") }

// symbolDeclared reports whether any declared name in the tree matches the
// symbol pattern. Patterns are matched against the bare declared name, which is
// why this is only ever asked of an UNQUALIFIED pattern.
func symbolDeclared(pat string, declared map[string]bool) bool {
	re, err := regexp.Compile(pat)
	if err != nil {
		return true // undecidable: stay silent
	}
	for name := range declared {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// funcsAnchorLive reports whether any function declared in an in-scope file has
// a name the funcs filter admits.
//
// This is a NAME question, not a call question, and the distinction is the whole
// arm. Deciding it by call counts — "the filter admitted nothing that made a
// call" — reads a matched function with an EMPTY BODY as a dead filter, because
// the engine only emits per call inside an admitted func. That is the same
// condemn-the-obedient inversion this file's header records for the symbol
// anchors. The engine's own funcAnchor asks the name question for
// call-order-in-func; this asks it for the type the engine skips.
func funcsAnchorLive(pat string, inScope []*scan.File) bool {
	re, err := regexp.Compile(pat)
	if err != nil {
		return true // undecidable: stay silent
	}
	for _, f := range inScope {
		if !strings.HasSuffix(f.Path(), ".go") {
			continue
		}
		lines, err := f.Lines()
		if err != nil {
			continue
		}
		for _, ln := range lines {
			if n := declName(ln); n != "" && re.MatchString(n) {
				return true
			}
		}
	}
	return false
}

// decidableAnchor reports whether an absent call site is enough to call this
// anchor dead: the pattern must name a repo-local symbol, and no declaration of
// it may survive anywhere in the tree.
func decidableAnchor(pat string, declared map[string]bool) bool {
	return pat != "" && !qualifiedPattern(pat) && !symbolDeclared(pat, declared)
}

// undecidedSymbolAnchors counts the name-anchored go rules whose subject anchor
// this instrument cannot decide at all: the pattern names another package, so an
// absent call site says nothing about whether the symbol can still be reached.
//
// Only the QUALIFIED ones are counted. An unqualified anchor whose subject is
// still declared IS decided — the answer is "alive" — and folding it in here
// would overstate the hole in the opposite direction.
//
// The count exists because a hole that goes unnamed reads as coverage, which is
// the defect this census exists to catch one level up. On this corpus the split
// is 11 decidable to 30 not, so a bare class-2 zero would report 41 rules
// cleared when 11 were examined.
func undecidedSymbolAnchors(cfg *config.Config) int {
	n := 0
	for _, r := range cfg.Rules {
		if symbolAnchorUndecidedReason(r) != "" {
			n++
		}
	}
	return n
}

// symbolAnchorUndecidedReason says WHY the census can render no vacuity verdict
// on a name-anchored go rule, or "" when it can. Single source for the question,
// same contract as relationUndecidedReason: report() counts through it and
// newrules.go refuses through it, so the admitted hole and the refused set
// cannot disagree.
func symbolAnchorUndecidedReason(r *config.Rule) string {
	// Type first: anchorParams re-renders and re-parses the rule's params, and
	// all but a few dozen of the corpus's rules are neither of these types.
	if r.Type != "go/call-confined-to-func-name" && r.Type != "go/guard-precedes-call" {
		return ""
	}
	p, err := anchorParams(r)
	if err != nil {
		return ""
	}
	pat, field := p.Symbol, "params.symbol"
	if r.Type == "go/guard-precedes-call" {
		pat, field = p.Sink, "params.sink"
	}
	if pat == "" {
		return "the rule declares no " + field + " anchor, so there is no name whose absence could be read"
	}
	if qualifiedPattern(pat) {
		// "package-qualified" is the common case but not the only one: measured on
		// this corpus, two of the anchors here qualify on a RECEIVER (`t`, `h`),
		// not a package. Both are undecidable for the same reason and the wording
		// must not claim otherwise — an author told their rule names another
		// package, when it names a receiver, goes looking for a package that was
		// never there.
		return fmt.Sprintf("the %s anchor %q is package-qualified, or qualified on a receiver — either way "+
			"the qualifier is a name the census has no type information to resolve, so an absent call site "+
			"cannot prove the symbol unreachable", field, pat)
	}
	return ""
}

// symbolAnchorVerdicts reports the anchors of r that match nothing in its live
// scope. It returns nil for every rule type that is not name-anchored, and nil
// for an anchor that is still live.
//
// The probe is a NEUTRALISED VARIANT, never a re-implementation of the matcher:
// recompile the rule with its exempting half defeated and re-run the real engine
// over the real in-scope set. Same discipline as satisfies/satisfiesSet in
// probe.go, and the same move config.Rule's own CloneWithChecker documents for
// lint's prefilter differential.
//
// Fixture copies of the symbol are excluded for free: scope include globs are
// root-anchored, and a fixture's copy lives under the rule's own fixture tree,
// which those globs do not match.
func symbolAnchorVerdicts(r *config.Rule, root string, inScope []*scan.File, declared map[string]bool) []verdict {
	switch r.Type {
	case "go/call-confined-to-func-name":
		return callConfinedVerdicts(r, root, inScope, declared)
	case "go/guard-precedes-call":
		return guardPrecedesVerdicts(r, root, inScope, declared)
	}
	return nil
}

// callConfinedVerdicts reports DEAD-SYMBOL: the confined symbol is declared
// nowhere in the tree AND, with allowed_func defeated so nothing is exempt,
// matches no call in scope. Both halves are load-bearing — see the file header.
//
// Defeating allowed_func is what separates "no call site left" from "every call
// site is where it belongs": a correctly confined rule is green because the
// symbol is live and every call sits in the allowed func, so asking only "does
// the rule pass?" would condemn every healthy confinement in the corpus.
func callConfinedVerdicts(r *config.Rule, root string, inScope []*scan.File, declared map[string]bool) []verdict {
	p, err := anchorParams(r)
	if err != nil || !decidableAnchor(p.Symbol, declared) {
		return nil
	}
	hits, err := anchorHits(r, root, inScope, map[string]string{"allowed_func": neverMatches})
	if err != nil || hits > 0 {
		return nil
	}
	return []verdict{{
		class: class2, code: "DEAD-SYMBOL", gating: true,
		why: "params.symbol is declared nowhere in the tree and matches no call in any in-scope file, " +
			"so nothing can ever violate the confinement and no tree can falsify this rule. The subject " +
			"was deleted and the rule kept reporting green. Repoint symbol at the name the code uses now, " +
			"or delete the rule, its fixtures and its registry rows together — a green shell is worse than " +
			"no rule, because the enumeration says the invariant is held",
		evidence: []string{"symbol: " + p.Symbol},
	}}
}

// guardPrecedesVerdicts reports DEAD-FUNCS or DEAD-SINK.
//
// The two anchors are decided differently, and deliberately so. `funcs` names
// FUNCTIONS IN SCOPE, so whether it admits anything is directly measurable —
// that is the same question the engine's own funcAnchor answers for
// call-order-in-func, and it needs no declaration test. `sink` names a called
// symbol, so it carries the full burden the file header describes.
//
// The `guard` pattern is not probed at all. A dead guard does not make the rule
// vacuous — it makes it maximally STRICT, flagging every sink call in scope.
// Reporting it would manufacture findings against rules enforcing harder than
// their author intended.
func guardPrecedesVerdicts(r *config.Rule, root string, inScope []*scan.File, declared map[string]bool) []verdict {
	p, err := anchorParams(r)
	if err != nil {
		return nil
	}
	if p.Funcs != "" && !funcsAnchorLive(p.Funcs, inScope) {
		return []verdict{{
			class: class2, code: "DEAD-FUNCS", gating: true,
			why: "params.funcs matches no function declared in any in-scope file, so no call ever enters " +
				"the obligation and the guard is never asked for. The filter outlived the functions it " +
				"named. Repoint funcs at the current names, or drop it if the sink should be guarded " +
				"everywhere",
			evidence: []string{"funcs: " + p.Funcs},
		}}
	}
	if !decidableAnchor(p.Sink, declared) {
		return nil
	}
	hits, err := anchorHits(r, root, inScope, map[string]string{"guard": neverMatches})
	if err != nil || hits > 0 {
		return nil
	}
	return []verdict{{
		class: class2, code: "DEAD-SINK", gating: true,
		why: "params.sink is declared nowhere in the tree and matches no call in any in-scope file, so " +
			"nothing ever enters the obligation and the guard requirement is never asked for. Repoint " +
			"sink at the call the code makes now, or delete the rule, its fixtures and its registry rows " +
			"together",
		evidence: []string{"sink: " + p.Sink},
	}}
}

// anchorParams reads the name-anchoring params off the rule as written. Only
// metadata is read here; every verdict above is still rendered by the engine.
func anchorParams(r *config.Rule) (anchorParamSet, error) {
	var p anchorParamSet
	raw, err := r.ParamsYAML()
	if err != nil || raw == "" {
		return p, err
	}
	if err := yaml.Unmarshal([]byte(raw), &p); err != nil {
		return p, err
	}
	return p, nil
}

// anchorParamSet is the subset of goast params this probe anchors on.
type anchorParamSet struct {
	Symbol string `yaml:"symbol"`
	Sink   string `yaml:"sink"`
	Funcs  string `yaml:"funcs"`
}

// anchorHits recompiles r with override applied to its params and returns how
// many findings the real engine produces over inScope.
//
// SUPPRESSED findings count. The question is whether the anchor can still see
// its subject, and a marker-suppressed finding proves it can — a new unmarked
// call would trip the rule tomorrow. Counting only unsuppressed findings would
// call a fully-marked live symbol dead and retire a working gate. This is the
// same reading exceptExcuses uses ("suppressed findings count as still trips").
//
// scope.exclude and except.paths are left as written, unlike Allowlist. A symbol
// surviving only in a path the rule excludes is invisible TO THE RULE.
func anchorHits(r *config.Rule, root string, inScope []*scan.File, override map[string]string) (int, error) {
	checker, err := recompileWith(r, override)
	if err != nil {
		return 0, err
	}
	probe := r.CloneWithChecker(checker)
	probe.Allowlist = nil
	fds, err := engine.Run([]*config.Rule{probe}, &scan.FileSet{Root: root, Files: inScope}, 1)
	if err != nil {
		return 0, err
	}
	return len(fds), nil
}

// recompileWith builds a checker for r's type from r's own params with override
// applied. The factory is the engine's — the census never constructs a checker
// of its own, so the variant is compiled by the same code that compiled the rule.
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
