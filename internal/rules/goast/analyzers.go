package goast

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// go/func-line-budget
// ---------------------------------------------------------------------------

type funcLineBudgetParams struct {
	MaxLines *int   `yaml:"max_lines"`
	Funcs    string `yaml:"funcs"`
}

type funcLineBudget struct {
	max   int
	funcs *regexp.Regexp
}

func newFuncLineBudget(node *yaml.Node) (rules.Checker, error) {
	var p funcLineBudgetParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.MaxLines == nil {
		return nil, errors.New("go/func-line-budget: params.max_lines is required")
	}
	if *p.MaxLines < 0 {
		return nil, fmt.Errorf("go/func-line-budget: params.max_lines must be >= 0, got %d", *p.MaxLines)
	}
	funcs, err := compileOptRe("go/func-line-budget", "funcs", p.Funcs)
	if err != nil {
		return nil, err
	}
	return &funcLineBudget{max: *p.MaxLines, funcs: funcs}, nil
}

// CheckFile flags each in-scope func whose body block spans more than max_lines
// lines. Body span is measured as (Rbrace line - Lbrace line) of the body
// block, so a single-line func body counts as 0.
func (c *funcLineBudget) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGo(f)
	if err != nil || !ok {
		return nil, err
	}
	var ms []rules.Match
	for _, fn := range extractFuncs(fset, file) {
		if !matchesFuncFilter(c.funcs, fn.name) {
			continue
		}
		if fn.bodySpan > c.max {
			ms = append(ms, rules.Match{
				Line:    fn.declLine,
				Message: fmt.Sprintf("func %q body spans %d line(s), budget is %d", fn.name, fn.bodySpan, c.max),
			})
		}
	}
	return ms, nil
}

// ---------------------------------------------------------------------------
// go/call-confined-to-func-name
// ---------------------------------------------------------------------------

type callConfinedParams struct {
	Symbol      string `yaml:"symbol"`
	AllowedFunc string `yaml:"allowed_func"`
}

type callConfined struct {
	symbol  *regexp.Regexp
	allowed *regexp.Regexp
}

func newCallConfined(node *yaml.Node) (rules.Checker, error) {
	var p callConfinedParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	symbol, err := compileRe("go/call-confined-to-func-name", "symbol", p.Symbol)
	if err != nil {
		return nil, err
	}
	allowed, err := compileRe("go/call-confined-to-func-name", "allowed_func", p.AllowedFunc)
	if err != nil {
		return nil, err
	}
	return &callConfined{symbol: symbol, allowed: allowed}, nil
}

// CheckFile flags any call whose selector matches symbol that appears inside a
// func whose name does not match allowed_func.
func (c *callConfined) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGo(f)
	if err != nil || !ok {
		return nil, err
	}
	var ms []rules.Match
	for _, fn := range extractFuncs(fset, file) {
		if c.allowed.MatchString(fn.name) {
			continue
		}
		for _, call := range fn.calls {
			if c.symbol.MatchString(call.selector) {
				ms = append(ms, rules.Match{
					Line: call.line,
					Message: fmt.Sprintf("call %q matching %q is not confined to funcs matching %q but appears in func %q",
						call.selector, c.symbol.String(), c.allowed.String(), fn.name),
				})
			}
		}
	}
	return ms, nil
}

// ---------------------------------------------------------------------------
// go/call-order-in-func
// ---------------------------------------------------------------------------

type callOrderParams struct {
	Funcs    string   `yaml:"funcs"`
	Sequence []string `yaml:"sequence"`
}

type callOrder struct {
	funcs *regexp.Regexp
	seq   []*regexp.Regexp

	funcAnchor rules.AnchorProbe   // funcs matched some func in scope
	seqAnchor  []rules.AnchorProbe // sequence[i] matched some call in scope
}

func newCallOrder(node *yaml.Node) (rules.Checker, error) {
	var p callOrderParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	funcs, err := compileRe("go/call-order-in-func", "funcs", p.Funcs)
	if err != nil {
		return nil, err
	}
	if len(p.Sequence) < 2 {
		return nil, errors.New("go/call-order-in-func: params.sequence requires at least 2 entries")
	}
	seq := make([]*regexp.Regexp, len(p.Sequence))
	for i, pat := range p.Sequence {
		re, err := compileRe("go/call-order-in-func", fmt.Sprintf("sequence[%d]", i), pat)
		if err != nil {
			return nil, err
		}
		seq[i] = re
	}
	return &callOrder{funcs: funcs, seq: seq, seqAnchor: make([]rules.AnchorProbe, len(seq))}, nil
}

// CheckFile verifies that within each matching func, all calls matching
// sequence[i] appear before any call matching sequence[i+1] (adjacent stages
// only). A stage with no matching call imposes no constraint on its neighbor.
func (c *callOrder) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGo(f)
	if err != nil || !ok {
		return nil, err
	}
	fns := extractFuncs(fset, file)
	c.probeAnchors(fns)
	var ms []rules.Match
	for _, fn := range fns {
		if !c.funcs.MatchString(fn.name) {
			continue
		}
		for i := 0; i+1 < len(c.seq); i++ {
			// lastI: the latest call matching stage i; firstJ: earliest matching stage i+1.
			lastIPos, lastILine, haveI := -1, 0, false
			firstJPos, firstJLine, haveJ := -1, 0, false
			for _, call := range fn.calls {
				if c.seq[i].MatchString(call.selector) {
					if !haveI || int(call.pos) > lastIPos {
						lastIPos, lastILine, haveI = int(call.pos), call.line, true
					}
				}
				if c.seq[i+1].MatchString(call.selector) {
					if !haveJ || int(call.pos) < firstJPos {
						firstJPos, firstJLine, haveJ = int(call.pos), call.line, true
					}
				}
			}
			if haveI && haveJ && lastIPos > firstJPos {
				ms = append(ms, rules.Match{
					Line: firstJLine,
					Message: fmt.Sprintf("in func %q: call matching %q must precede call matching %q (out of order at line %d)",
						fn.name, c.seq[i].String(), c.seq[i+1].String(), lastILine),
				})
			}
		}
	}
	return ms, nil
}

// probeAnchors records, over the whole scope, whether the funcs anchor still
// names a func and whether each sequence stage still names a call. Stage
// probes run over EVERY func in the file, not only the in-scope ones, so the
// two verdicts stay independent: a dead funcs anchor is one finding about one
// rename, not one plus a stage report per sequence entry.
func (c *callOrder) probeAnchors(fns []funcInfo) {
	c.funcAnchor.Observe()
	for i := range c.seqAnchor {
		c.seqAnchor[i].Observe()
	}
	for _, fn := range fns {
		if c.funcs.MatchString(fn.name) {
			c.funcAnchor.Hit()
		}
		for _, call := range fn.calls {
			for i, re := range c.seq {
				if re.MatchString(call.selector) {
					c.seqAnchor[i].Hit()
				}
			}
		}
	}
}

// Finalize reports a dead funcs anchor or a dead sequence stage. Unlike a
// count relation, a call-order sequence has no reading in which a missing
// stage is the compliant state — the stage imposes no ordering at all when
// nothing matches it — so both assertions are default-on with no opt-out.
func (c *callOrder) Finalize() []rules.Match {
	ms := c.funcAnchor.Verdict("funcs anchor", c.funcs.String())
	for i := range c.seq {
		ms = append(ms, c.seqAnchor[i].Verdict(fmt.Sprintf("sequence[%d]", i), c.seq[i].String())...)
	}
	return ms
}

// WholeTreeInvariant is true: the anchor verdicts are "no in-scope file
// matched", which is non-monotonic under file removal (spec §8).
func (c *callOrder) WholeTreeInvariant() bool { return true }

// ---------------------------------------------------------------------------
// go/guard-precedes-call
// ---------------------------------------------------------------------------

type guardPrecedesParams struct {
	Guard string `yaml:"guard"`
	Sink  string `yaml:"sink"`
	Funcs string `yaml:"funcs"`
}

type guardPrecedes struct {
	guard *regexp.Regexp
	sink  *regexp.Regexp
	funcs *regexp.Regexp
}

func newGuardPrecedes(node *yaml.Node) (rules.Checker, error) {
	var p guardPrecedesParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	guard, err := compileRe("go/guard-precedes-call", "guard", p.Guard)
	if err != nil {
		return nil, err
	}
	sink, err := compileRe("go/guard-precedes-call", "sink", p.Sink)
	if err != nil {
		return nil, err
	}
	funcs, err := compileOptRe("go/guard-precedes-call", "funcs", p.Funcs)
	if err != nil {
		return nil, err
	}
	return &guardPrecedes{guard: guard, sink: sink, funcs: funcs}, nil
}

// CheckFile flags every sink call in an in-scope func that is not preceded (by
// source position) by at least one guard call earlier in the same func. A call
// matching both patterns does not guard itself.
func (c *guardPrecedes) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGo(f)
	if err != nil || !ok {
		return nil, err
	}
	var ms []rules.Match
	for _, fn := range extractFuncs(fset, file) {
		if !matchesFuncFilter(c.funcs, fn.name) {
			continue
		}
		guardSeen := false
		for _, call := range fn.calls {
			if c.sink.MatchString(call.selector) && !guardSeen {
				ms = append(ms, rules.Match{
					Line: call.line,
					Message: fmt.Sprintf("in func %q: sink call %q is not preceded by a guard matching %q",
						fn.name, call.selector, c.guard.String()),
				})
			}
			if c.guard.MatchString(call.selector) {
				guardSeen = true
			}
		}
	}
	return ms, nil
}

// ---------------------------------------------------------------------------
// go/per-func-count-relation
// ---------------------------------------------------------------------------

type countRelationParams struct {
	Left          string `yaml:"left"`
	Right         string `yaml:"right"`
	Relation      string `yaml:"relation"`
	Funcs         string `yaml:"funcs"`
	RequireSymbol string `yaml:"require_symbol"`
	// RequireUsed, when set to left/right/both, only counts a matching call
	// on that side when the call's results are consumed (not a bare ExprStmt
	// and not `_ = …`). A security gate that counts RequireEnabled must set
	// this on the gate side; otherwise a discarded `RequireEnabled(...)`
	// satisfies the relation with no enforcement.
	RequireUsed string `yaml:"require_used"`
}

const (
	relLE = "<="
	relEQ = "=="
	relGE = ">="
)

const (
	symLeft  = "left"
	symRight = "right"
	symBoth  = "both"
)

type countRelation struct {
	left   *regexp.Regexp
	right  *regexp.Regexp
	rel    string
	funcs  *regexp.Regexp
	reqSym string
	reqUse string // "", left, right, both — sides that require resultUsed

	funcAnchor  rules.AnchorProbe // funcs matched some func in scope
	leftAnchor  rules.AnchorProbe // left matched some call selector in scope
	rightAnchor rules.AnchorProbe // right matched some call selector in scope
}

func newCountRelation(node *yaml.Node) (rules.Checker, error) {
	var p countRelationParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	left, err := compileRe("go/per-func-count-relation", "left", p.Left)
	if err != nil {
		return nil, err
	}
	right, err := compileRe("go/per-func-count-relation", "right", p.Right)
	if err != nil {
		return nil, err
	}
	switch p.Relation {
	case "":
		return nil, errors.New("go/per-func-count-relation: params.relation is required")
	case relLE, relEQ, relGE:
	default:
		return nil, fmt.Errorf("go/per-func-count-relation: unknown relation %q (want %q, %q, or %q)", p.Relation, relLE, relEQ, relGE)
	}
	funcs, err := compileOptRe("go/per-func-count-relation", "funcs", p.Funcs)
	if err != nil {
		return nil, err
	}
	switch p.RequireSymbol {
	case "", symLeft, symRight, symBoth:
	default:
		return nil, fmt.Errorf("go/per-func-count-relation: unknown require_symbol %q (want %q, %q, or %q)", p.RequireSymbol, symLeft, symRight, symBoth)
	}
	switch p.RequireUsed {
	case "", symLeft, symRight, symBoth:
	default:
		return nil, fmt.Errorf("go/per-func-count-relation: unknown require_used %q (want %q, %q, or %q)", p.RequireUsed, symLeft, symRight, symBoth)
	}
	return &countRelation{
		left: left, right: right, rel: p.Relation, funcs: funcs,
		reqSym: p.RequireSymbol, reqUse: p.RequireUsed,
	}, nil
}

// counts reports whether call contributes to the left/right tally, honouring
// require_used when set for that side.
func (c *countRelation) counts(call callInfo, side string, re *regexp.Regexp) bool {
	if !re.MatchString(call.selector) {
		return false
	}
	needUsed := c.reqUse == side || c.reqUse == symBoth
	if needUsed && !call.resultUsed {
		return false
	}
	return true
}

// CheckFile asserts, per in-scope func, count(left) relation count(right) over
// the func's call selectors, flagging the func when the relation does not hold.
func (c *countRelation) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGo(f)
	if err != nil || !ok {
		return nil, err
	}
	fns := extractFuncs(fset, file)
	c.probeAnchors(fns)
	var ms []rules.Match
	for _, fn := range fns {
		if !matchesFuncFilter(c.funcs, fn.name) {
			continue
		}
		var lc, rc int
		for _, call := range fn.calls {
			if c.counts(call, symLeft, c.left) {
				lc++
			}
			if c.counts(call, symRight, c.right) {
				rc++
			}
		}
		ok := true
		switch c.rel {
		case relLE:
			ok = lc <= rc
		case relEQ:
			ok = lc == rc
		case relGE:
			ok = lc >= rc
		}
		if !ok {
			ms = append(ms, rules.Match{
				Line: fn.declLine,
				Message: fmt.Sprintf("func %q: count(%q)=%d %s count(%q)=%d does not hold",
					fn.name, c.left.String(), lc, c.rel, c.right.String(), rc),
			})
		}
	}
	return ms, nil
}

// probeAnchors records whether the funcs anchor still names a func, and
// whether the required side(s) of the relation still name a call, anywhere in
// scope. Symbol probes run over EVERY func in the file rather than the
// filtered ones so the two verdicts stay independent claims about two
// different renames. require_used does not apply to the anchor: a discarded
// call still proves the symbol exists under its spelling.
func (c *countRelation) probeAnchors(fns []funcInfo) {
	if c.funcs != nil {
		c.funcAnchor.Observe()
	}
	if c.reqSym != "" {
		c.leftAnchor.Observe()
		c.rightAnchor.Observe()
	}
	for _, fn := range fns {
		if c.funcs != nil && c.funcs.MatchString(fn.name) {
			c.funcAnchor.Hit()
		}
		if c.reqSym == "" {
			continue
		}
		for _, call := range fn.calls {
			if c.left.MatchString(call.selector) {
				c.leftAnchor.Hit()
			}
			if c.right.MatchString(call.selector) {
				c.rightAnchor.Hit()
			}
		}
	}
}

// Finalize reports a dead funcs anchor, plus a dead side of the relation when
// require_symbol asks for it.
//
// The funcs anchor is default-on because a name that matches nothing is always
// a defect. The SYMBOL assertion is opt-in because absence is sometimes the
// compliant state: the forbidden-call-in-func idiom (`funcs: ^Name$`, left: a
// set of banned calls, right: a never-matching sentinel, relation: <=) is
// satisfied precisely BY left matching nothing. Which side constrains is the
// author's judgment, so the engine takes it as a declaration rather than
// inferring it from the relation.
func (c *countRelation) Finalize() []rules.Match {
	var ms []rules.Match
	if c.funcs != nil {
		ms = append(ms, c.funcAnchor.Verdict("funcs anchor", c.funcs.String())...)
	}
	if c.reqSym == symLeft || c.reqSym == symBoth {
		ms = append(ms, c.leftAnchor.Verdict("require_symbol: left", c.left.String())...)
	}
	if c.reqSym == symRight || c.reqSym == symBoth {
		ms = append(ms, c.rightAnchor.Verdict("require_symbol: right", c.right.String())...)
	}
	return ms
}

// WholeTreeInvariant is true only when there is an anchor verdict to report:
// "no in-scope file matched" is non-monotonic under file removal, while the
// per-func relation itself stays range-scopeable (spec §8).
func (c *countRelation) WholeTreeInvariant() bool { return c.funcs != nil || c.reqSym != "" }

// ---------------------------------------------------------------------------

func init() {
	rules.Register("go/func-line-budget", newFuncLineBudget)
	rules.Register("go/call-confined-to-func-name", newCallConfined)
	rules.Register("go/call-order-in-func", newCallOrder)
	rules.Register("go/guard-precedes-call", newGuardPrecedes)
	rules.Register("go/per-func-count-relation", newCountRelation)
}
