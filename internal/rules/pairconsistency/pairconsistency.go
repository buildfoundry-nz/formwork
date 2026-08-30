// Package pairconsistency implements the `pair-consistency` rule type
// (spec §5): a `trigger` match obliges a `requires` match within the same
// UNIT. `where: same-file` (the default) makes the unit one file, and the rule
// is then a pure per-file Checker. `where: same-dir` makes the unit the
// containing directory — the package, for Go — which needs a cross-file
// finalizer and is therefore a whole-tree invariant. `where: same-func`
// makes the unit one function-grain span: a top-level Go function body
// (go/parser spans, so a multi-line signature cannot blind the unit the way
// the retired shell brace-depth accumulators did — a defect the validating
// port hit, lifted upstream in #77), a Dart function/method body, or a proto
// message/enum/service/rpc block (brace-depth units, added for the first
// production corpus).
package pairconsistency

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type pairParams struct {
	Trigger  string `yaml:"trigger"`
	Requires string `yaml:"requires"`
	Where    string `yaml:"where"`
	// AlsoPresent, when set, is an additional regex the unit must match
	// before the trigger obliges requires. same-func uses it to restore the
	// retired shell's has_query arm (`tx.(Query|QueryRow)(`): Queue* siblings
	// name the SQL builder without issuing a query and must not oblige Memo.
	AlsoPresent string `yaml:"also_present"`
	// Obligation selects how many companions a unit owes when the trigger
	// matches. "" / "presence" (default): one requires match clears the unit
	// regardless of trigger count. "countable": count(requires) must be ≥
	// count(trigger) inside the unit so a second trigger cannot free-ride on a
	// single companion (vacuity class V18). same-dir is refused
	// under countable — that mode only accumulates presence today.
	Obligation string `yaml:"obligation"`
}

const (
	obligationPresence  = "presence"
	obligationCountable = "countable"
)

const (
	// whereSameFile: the trigger and its companion must share one file. A
	// per-file assertion, so it stays changeset-safe. Also the default when
	// `where:` is omitted.
	whereSameFile = "same-file"
	// whereSameDir: the unit is the containing DIRECTORY, which for Go is the
	// package. Some invariants are satisfied by a package COLLECTIVELY: one
	// file assigns a value and a shared tail in a sibling file answers with it,
	// one tail serving several handlers. The
	// examples/palletra-port-full/.formwork/rules/derived-dispatch-claims-ledger.yaml
	// shape is the canonical one — the auto-run engine enqueues in engine.go
	// and records the claim in plan.go, two files reviewed as a unit — and a
	// same-file rule flags the enqueuing file, condemning a package that is
	// correct. Widening the unit is the fix; a scope exclude would be a
	// carve-out. The verdict joins files, which is why it is a whole-tree
	// invariant.
	whereSameDir = "same-dir"
	// whereSameFunc: the unit is one function-grain span, extracted per
	// language (units.go): for Go, one top-level function (including
	// methods), bounded by go/parser so a multi-line signature and a
	// brace-bearing parameter type (`opts struct{ A int }`) cannot close the
	// unit early, and nested func-literal bodies (Memo closures) sit inside
	// the outer span; for Dart, one function/method body; for proto, one
	// message/enum/service/rpc block. Range-scopeable.
	whereSameFunc = "same-func"
)

// dirState accumulates one directory's evidence across the concurrent per-file
// pass: the earliest trigger seen (path + line, to anchor the finding) and
// whether any file in the directory carried the companion.
type dirState struct {
	triggerPath string
	triggerLine int
	requires    bool
}

type pairConsistency struct {
	trigger     *regexp.Regexp
	requires    *regexp.Regexp
	alsoPresent *regexp.Regexp // optional; nil when unset
	where       string
	obligation  string

	// dirs is written from CheckFile, which the engine runs from a worker pool,
	// so mu guards it. Populated in same-dir mode only.
	mu   sync.Mutex
	dirs map[string]*dirState
}

func newPairConsistency(params *yaml.Node) (rules.Checker, error) {
	var p pairParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Trigger == "" {
		return nil, errors.New("pair-consistency: params.trigger is required")
	}
	if p.Requires == "" {
		return nil, errors.New("pair-consistency: params.requires is required")
	}
	where := p.Where
	if where == "" {
		where = whereSameFile
	}
	if where != whereSameFile && where != whereSameDir && where != whereSameFunc {
		return nil, fmt.Errorf("pair-consistency: unknown where %q (want %q, %q, or %q)",
			p.Where, whereSameFile, whereSameDir, whereSameFunc)
	}
	// also_present gates the obligation on the unit carrying some OTHER marker,
	// which is coherent for any unit that holds a text span. same-func's span is
	// the function body; same-file's is the file. same-dir's unit is a DIRECTORY
	// assembled across files in Finalize, so there is no one span to ask about —
	// and a gate that cannot be evaluated must refuse at config time rather than
	// resolve to "not owed", which would be the fail-open direction. Refused for
	// that mode only (#189).
	if p.AlsoPresent != "" && where == whereSameDir {
		return nil, fmt.Errorf("pair-consistency: also_present is not valid with where: %q — its unit is the directory, assembled across files, so no single span carries the marker; use %q or %q",
			whereSameDir, whereSameFile, whereSameFunc)
	}
	obligation := p.Obligation
	if obligation == "" {
		obligation = obligationPresence
	}
	if obligation != obligationPresence && obligation != obligationCountable {
		return nil, fmt.Errorf("pair-consistency: unknown obligation %q (want %q or %q)",
			p.Obligation, obligationPresence, obligationCountable)
	}
	if obligation == obligationCountable && where == whereSameDir {
		return nil, fmt.Errorf("pair-consistency: obligation %q is not supported with where: %q (use %q or %q)",
			obligationCountable, whereSameDir, whereSameFile, whereSameFunc)
	}
	trigger, err := regexp.Compile(p.Trigger)
	if err != nil {
		return nil, fmt.Errorf("pair-consistency: invalid trigger: %w", err)
	}
	requires, err := regexp.Compile(p.Requires)
	if err != nil {
		return nil, fmt.Errorf("pair-consistency: invalid requires: %w", err)
	}
	var alsoPresent *regexp.Regexp
	if p.AlsoPresent != "" {
		alsoPresent, err = regexp.Compile(p.AlsoPresent)
		if err != nil {
			return nil, fmt.Errorf("pair-consistency: invalid also_present: %w", err)
		}
	}
	return &pairConsistency{
		trigger:     trigger,
		requires:    requires,
		alsoPresent: alsoPresent,
		where:       where,
		obligation:  obligation,
		dirs:        map[string]*dirState{},
	}, nil
}

// scanFile returns the first trigger line (0 when absent), whether the companion
// appears anywhere in the file, and the multiset counts used by obligation:countable.
// fileCarriesAlsoPresent reports whether the file holds the also_present
// marker. Line-wise like scanFile rather than over joined content, so the two
// agree about what "in this file" means and neither can match across a line
// boundary the other cannot.
func (c *pairConsistency) fileCarriesAlsoPresent(f *scan.File) (bool, error) {
	lines, err := f.Lines()
	if err != nil {
		return false, err
	}
	for _, line := range lines {
		if c.alsoPresent.MatchString(line) {
			return true, nil
		}
	}
	return false, nil
}

func (c *pairConsistency) scanFile(f *scan.File) (triggerLine int, requiresMatched bool, nTrigger, nRequires int, err error) {
	lines, err := f.Lines()
	if err != nil {
		return 0, false, 0, 0, err
	}
	for i, line := range lines {
		if c.trigger.MatchString(line) {
			nTrigger++
			if triggerLine == 0 {
				triggerLine = i + 1
			}
		}
		if c.requires.MatchString(line) {
			nRequires++
			requiresMatched = true
		}
	}
	return triggerLine, requiresMatched, nTrigger, nRequires, nil
}

// CheckFile flags a unit whose trigger matches but whose requires does not.
// same-file: the unit is the file; reports the first trigger line.
// same-func: the unit is each function-grain span the file's language has
// (Go function, Dart method, proto block); one finding per bare unit, named
// in the message.
// same-dir: the file cannot be judged alone — evidence folds into the
// directory state and the verdict waits for Finalize.
func (c *pairConsistency) CheckFile(f *scan.File) ([]rules.Match, error) {
	if c.where == whereSameFunc {
		return c.checkSameFunc(f)
	}
	triggerLine, requiresMatched, nTrigger, nRequires, err := c.scanFile(f)
	if err != nil {
		return nil, err
	}
	if c.where == whereSameDir {
		c.recordDir(f.Path(), triggerLine, requiresMatched)
		return nil, nil
	}
	if triggerLine == 0 {
		return nil, nil
	}
	// same-file's span is the file. Asked only once a trigger has matched, so a
	// file the rule does not speak to is never read a second time (#189).
	if c.alsoPresent != nil {
		gated, err := c.fileCarriesAlsoPresent(f)
		if err != nil {
			return nil, err
		}
		if !gated {
			return nil, nil
		}
	}
	if c.obligation == obligationCountable {
		if nRequires < nTrigger {
			return []rules.Match{{
				Line: triggerLine,
				Message: fmt.Sprintf(
					"pair-consistency: countable obligation: trigger %s matched %d time(s) but required companion %s matched only %d time(s) in the same file",
					c.trigger.String(), nTrigger, c.requires.String(), nRequires,
				),
			}}, nil
		}
		return nil, nil
	}
	if !requiresMatched {
		return []rules.Match{{
			Line:    triggerLine,
			Message: "pair-consistency: trigger " + c.trigger.String() + " matched but required companion " + c.requires.String() + " is missing from the same file",
		}}, nil
	}
	return nil, nil
}

// checkSameFunc judges each unit sameFuncUnits extracts for the file's
// language (.go, .dart, .proto). A file of any other extension yields no
// findings — same-func has no unit vocabulary for it, the same degradation
// contract as the go/* analyzers (goast parseGo). A parse failure (Go
// syntax error, unbalanced braces in a Dart/proto unit) is an engine error
// so a broken fixture cannot silently pass.
func (c *pairConsistency) checkSameFunc(f *scan.File) ([]rules.Match, error) {
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	units, err := sameFuncUnits(f.Path(), content)
	if err != nil {
		return nil, err
	}
	var ms []rules.Match
	for _, u := range units {
		m, err := c.checkFuncSpan(f, content, u)
		if err != nil {
			return nil, err
		}
		if m != nil {
			ms = append(ms, *m)
		}
	}
	return ms, nil
}

// checkFuncSpan judges one function-grain unit against
// trigger/also_present/requires. nil, nil means the unit passes; a non-nil
// Match is anchored on the first trigger line in the span.
func (c *pairConsistency) checkFuncSpan(f *scan.File, content []byte, u funcUnit) (*rules.Match, error) {
	if u.start < 0 || u.end > len(content) || u.start >= u.end {
		// Impossible: offsets come from parsing this exact content. If it
		// ever fires, something is deeply wrong — fail the run, never
		// silently skip a function that may owe a finding.
		return nil, fmt.Errorf("pair-consistency same-func: %s: unit %s span [%d:%d) outside content (len %d)",
			f.Path(), u.name, u.start, u.end, len(content))
	}
	span := string(content[u.start:u.end])
	nTrigger := len(c.trigger.FindAllStringIndex(span, -1))
	if nTrigger == 0 {
		return nil, nil
	}
	if c.alsoPresent != nil && !c.alsoPresent.MatchString(span) {
		return nil, nil
	}
	nRequires := len(c.requires.FindAllStringIndex(span, -1))
	// Anchor on the first trigger line inside the span (1-based file line).
	line := u.line
	if loc := c.trigger.FindStringIndex(span); loc != nil {
		line = u.line + strings.Count(span[:loc[0]], "\n")
	}
	if c.obligation == obligationCountable {
		if nRequires < nTrigger {
			return &rules.Match{
				Line: line,
				Message: fmt.Sprintf(
					"pair-consistency: func %q: countable obligation: trigger %s matched %d time(s) but required companion %s matched only %d time(s) in the same function",
					u.name, c.trigger.String(), nTrigger, c.requires.String(), nRequires,
				),
			}, nil
		}
		return nil, nil
	}
	if nRequires > 0 {
		return nil, nil
	}
	return &rules.Match{
		Line: line,
		Message: fmt.Sprintf(
			"pair-consistency: func %q: trigger %s matched but required companion %s is missing from the same function",
			u.name, c.trigger.String(), c.requires.String(),
		),
	}, nil
}

// recordDir folds one file's evidence into its directory's state. The anchor is
// the FIRST triggering file in path order, so the reported location is stable
// regardless of the order the worker pool happens to visit files in.
func (c *pairConsistency) recordDir(filePath string, triggerLine int, requiresMatched bool) {
	dir := path.Dir(filePath)
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.dirs[dir]
	if st == nil {
		st = &dirState{}
		c.dirs[dir] = st
	}
	if requiresMatched {
		st.requires = true
	}
	if triggerLine != 0 && (st.triggerPath == "" || filePath < st.triggerPath) {
		st.triggerPath, st.triggerLine = filePath, triggerLine
	}
}

// Finalize reports one finding per directory that carried the trigger and no
// companion, anchored on the first triggering file and emitted in directory
// order. same-file accumulates no state and returns nothing here.
func (c *pairConsistency) Finalize() []rules.Match {
	if c.where != whereSameDir {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	offenders := make([]string, 0, len(c.dirs))
	for dir, st := range c.dirs {
		if st.triggerPath != "" && !st.requires {
			offenders = append(offenders, dir)
		}
	}
	sort.Strings(offenders)
	out := make([]rules.Match, 0, len(offenders))
	for _, dir := range offenders {
		st := c.dirs[dir]
		out = append(out, rules.Match{
			Path: st.triggerPath,
			Line: st.triggerLine,
			Message: "pair-consistency: trigger " + c.trigger.String() +
				" matched but required companion " + c.requires.String() +
				" is missing from every file in the directory " + dir,
		})
	}
	return out
}

// WholeTreeInvariant reports true only in same-dir mode: that verdict is
// computed over every in-scope file of a directory, so a changeset-restricted
// FileSet (--staged / --range) routinely holds the trigger's file while the
// companion sits in an untouched sibling, and the rule would false-fail every
// commit that touches the trigger file. Same non-monotonic class as
// required-pattern(exists), pattern-count, set-relation and baseline (#4).
// same-file is a per-file assertion and stays range-scopeable.
func (c *pairConsistency) WholeTreeInvariant() bool { return c.where == whereSameDir }

func init() {
	rules.Register("pair-consistency", newPairConsistency)
}
