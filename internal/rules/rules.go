// Package rules defines the checker contract and the registry mapping YAML
// `type:` strings to implementations (spec §4).
package rules

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// Match is one raw hit from a checker. Path is optional: when empty, the
// engine substitutes the path of the file being checked; finalizers may set
// it (or leave it empty for scope-level findings).
type Match struct {
	Path    string
	Line    int
	Message string
}

// Checker evaluates one rule. CheckFile must be safe for concurrent use:
// the engine calls it from a worker pool.
type Checker interface {
	CheckFile(f *scan.File) ([]Match, error)
}

// Finalizer is implemented by checkers that need a pass after every in-scope
// file has been seen (e.g. required-pattern in exists mode).
type Finalizer interface {
	Finalize() []Match
}

// FinalizeContext carries run-scoped facts a whole-run rule needs but the
// per-file Checker path does not — chiefly the repository root external-tool
// rules (`command`, `git-diff`) must run in.
type FinalizeContext struct {
	Root string
}

// ErrFinalizer is a Finalizer that may fail and needs run context. Rule types
// that execute external tools (`command`, `git-diff`) report an execution
// failure — a missing binary, a git error — as an engine error (exit 2), never
// as a silent pass (spec §11). The engine prefers ErrFinalizer over Finalizer
// when a checker implements both.
type ErrFinalizer interface {
	FinalizeErr(FinalizeContext) ([]Match, error)
}

// Cost classifies a rule type's evaluation cost (spec §8). Fast rules are
// cheap per-file scans safe to run on every commit; heavy rules shell out to
// external tools or git and belong to heavier lanes. A Checker that does not
// implement Coster is CostFast.
type Cost string

const (
	CostFast  Cost = "fast"
	CostHeavy Cost = "heavy"
)

// Coster is optionally implemented by a Checker to declare its cost class.
type Coster interface {
	Cost() Cost
}

// CostOf returns c's declared cost, defaulting to CostFast.
func CostOf(c Checker) Cost {
	if cc, ok := c.(Coster); ok {
		return cc.Cost()
	}
	return CostFast
}

// ProcessBound is optionally implemented by a CostHeavy checker whose
// subprocess footprint is per-process RAM (Dart/Flutter analyzers). The
// engine caps those at heavyFinalizerWorkers. A CostHeavy checker that
// does not implement ProcessBound defaults to bound — the conservative
// #67 default so an unknown heavy type cannot fork unbounded analyzers.
//
// command implements it, and OPTS OUT of the conservative default above for
// the one rule type that can spawn an arbitrary program: it binds only what
// it can recognise as an analyzer (#81). A wrapper naming no analyzer is
// still unbound — see ProcessBound's comment and #236.
//
// git-diff implements it as not bound (git is not multi-GB).
type ProcessBound interface {
	ProcessBound() bool
}

// ProcessBoundOf reports whether c belongs in the width-capped analyzer
// pool. CostFast is never bound. CostHeavy without the interface is bound.
func ProcessBoundOf(c Checker) bool {
	if CostOf(c) != CostHeavy {
		return false
	}
	if p, ok := c.(ProcessBound); ok {
		return p.ProcessBound()
	}
	return true
}

// Prefiltered is optionally implemented by a checker carrying a literal
// prefilter gate (spec §5): a cheap substring test that skips a file before the
// real matcher runs. WithoutPrefilter returns an equivalent checker with the
// gate removed, for lint's load-bearing-prefilter differential (spec §9). It is
// for STATELESS per-file checkers only — WithoutPrefilter may share state via a
// shallow copy, so a stateful checker must instead rebuild via its factory.
type Prefiltered interface {
	Prefilter() string
	WithoutPrefilter() Checker
}

// PrefilterOf reports c's prefilter literal if it carries one. Mirrors CostOf /
// IsWholeTreeInvariant: the single place that knows a checker gates and on what
// literal. ok is false (literal "") when c is not Prefiltered or its prefilter
// is empty.
func PrefilterOf(c Checker) (literal string, ok bool) {
	p, isP := c.(Prefiltered)
	if !isP {
		return "", false
	}
	lit := p.Prefilter()
	return lit, lit != ""
}

// PrefilterImplication is optionally implemented by a Prefiltered checker that
// can decide, from its own compiled pattern alone, whether a match is possible
// without the prefilter literal. A prefilter is a pure optimization exactly
// when it is implied — no data required to know it.
//
// This is what lets lint judge a prefilter on a rule with zero findings and no
// fixtures, where both differentials are silent (#133).
//
// The contract is deliberately three-valued, not two. decidable=false means
// "this checker cannot tell" — a regexp2 pattern it does not parse, a
// case-folded literal, a branch with no guaranteed literal — and is NEVER on
// its own a finding. Only implied=false with decidable=true is a defect, and
// it must come with a counterexample naming the branch that can match without
// the literal. A purely lexical version of this test was considered and
// rejected in the 2026-07-27 design as noisy and wrong on escaping; an
// implementation earns the right to report by parsing, and by staying silent
// whenever it is unsure.
type PrefilterImplication interface {
	PrefilterImplied() (implied, decidable bool, counterexample string)
}

// PrefilterImpliedBy reports whether c proves its own prefilter redundant.
// Mirrors PrefilterOf: the single place that knows how to ask. decidable is
// false for any checker not implementing PrefilterImplication, so a new
// Prefiltered checker that never opts in degrades to "no static evidence"
// rather than to a silent pass.
func PrefilterImpliedBy(c Checker) (implied, decidable bool, counterexample string) {
	p, ok := c.(PrefilterImplication)
	if !ok {
		return false, false, ""
	}
	return p.PrefilterImplied()
}

// SkipReporter is optionally implemented by a Checker that can DECLINE TO RUN
// ITSELF — a gate of its own, expressed in its params rather than in the rule's
// scope, that the engine and the scope predicate know nothing about. `command`'s
// `when.paths_changed` is the gate it was written for.
//
// The contract is "did you skip, and why": reason must be a complete, operator-
// readable sentence naming the gate and the fact that the work did not happen,
// because it is rendered verbatim beside the rule id and is the entire cure
// surface of the disclosure. skipped must be reported only after the checker has
// actually declined — a checker that has not reached its decision must answer
// false, so the disclosure describes the run rather than predicting it.
//
// It exists because such a skip is indistinguishable, in every renderer, from a
// rule that ran and passed: `[id] OK`, exit 0, tool never executed (#159). The
// skip stays correct; only its silence is the defect.
//
// ORDERING IS A CALLER OBLIGATION, and this is where it differs from Coster and
// Prefiltered: those answer from static config and are pure, so the moment you
// ask is irrelevant. SkipReasonOf reads MUTABLE RUN STATE. Asked before the
// engine has run the checker, it answers "no skip" — indistinguishable from a
// rule that ran — so a caller must ask after the run whose skips it is
// reporting, and the requirement that a checker answer false until it has
// declined is what makes the early answer merely empty rather than wrong.
type SkipReporter interface {
	SkipReason() (reason string, skipped bool)
}

// SkipReasonOf reports whether c declined to run itself, and why. Mirrors CostOf
// / PrefilterOf: the single place that knows how to ask. A Checker that does not
// implement SkipReporter never skips itself as far as callers are concerned,
// which is the only sound default — nothing else in the package can tell.
//
// Unlike PrefilterOf, an empty reason does NOT downgrade skipped to false: that
// would turn a checker's under-documented skip into no skip at all, and this
// interface exists precisely because an unreported skip reads as a pass.
// Rendering an empty reason is the caller's problem to make loud.
func SkipReasonOf(c Checker) (reason string, skipped bool) {
	s, ok := c.(SkipReporter)
	if !ok {
		return "", false
	}
	return s.SkipReason()
}

// ValidCost reports whether s names a cost class.
func ValidCost(s string) bool {
	return Cost(s) == CostFast || Cost(s) == CostHeavy
}

// WholeTreeInvariant is optionally implemented by a Checker whose verdict is a
// whole-repo invariant: it depends on the ABSENCE of a pattern anywhere in
// scope (required-pattern in exists mode), a cross-file join (set-relation), a
// scope-wide count (pattern-count), or a tracked-set comparison (baseline).
// Such a rule is NON-MONOTONIC under file removal — restricting the input to a
// changeset can flip a true pass into a false "not found" / "subset violated" /
// "count off" finding merely because the file bearing the token is not in the
// change range. The CLI therefore evaluates these rules over the WHOLE tree
// even under --staged/--range, while range-scoping the per-file (monotonic)
// rules for speed. A Checker that does not implement this — forbidden-pattern,
// required-pattern in every-file mode, and every per-file scanner — is
// range-scopeable: removing files can only remove its findings, never add one.
type WholeTreeInvariant interface {
	WholeTreeInvariant() bool
}

// IsWholeTreeInvariant reports whether c's verdict is a whole-repo invariant
// that must be evaluated over the whole tree even under a changeset scan.
// Defaults to false (range-scopeable) for checkers that do not declare it.
func IsWholeTreeInvariant(c Checker) bool {
	w, ok := c.(WholeTreeInvariant)
	return ok && w.WholeTreeInvariant()
}

// Factory builds a Checker from a rule's params node (may be nil).
type Factory func(params *yaml.Node) (Checker, error)

// registry is written only by Register during package init (rule-type
// packages register themselves in init functions) and is read-only
// afterwards, so it needs no synchronization. Do not call Register after
// program startup.
var registry = map[string]Factory{}

// Register adds a rule type. Duplicate registration is a programming error.
func Register(name string, f Factory) {
	if _, dup := registry[name]; dup {
		panic("rules: duplicate registration of type " + name)
	}
	registry[name] = f
}

// Lookup resolves a rule type name.
func Lookup(name string) (Factory, bool) {
	f, ok := registry[name]
	return f, ok
}

// TypeNames lists registered types, sorted, for error messages.
func TypeNames() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// DecodeParams strictly decodes a params node into out; unknown fields are
// errors. A nil or zero node leaves out at its zero value.
func DecodeParams(n *yaml.Node, out any) error {
	if n == nil || n.Kind == 0 {
		return nil
	}
	raw, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}
