// Package gotypes is the go/types rule family: rules that resolve what a Go
// name refers to, which the syntax-only go/* family cannot. The analyzers run
// under `go vet`, with the formwork binary as the vettool, so the go command
// builds every package's type information and caches the results.
// Design: docs/specs/2026-11-19-go-types-rule-family-design.md.
package gotypes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/unitchecker"
)

// VettoolEnv names the variable formwork sets when it runs `go vet` with
// itself as the vettool. main hands control to the analyzers when it is "1".
const VettoolEnv = "FORMWORK_VETTOOL"

// planFlag is the one rule a `go vet` run evaluates, as JSON. It travels as an
// analyzer flag, never in the environment: the go command keys its cached vet
// results on the flags it passes the vettool, and not on the environment.
var planFlag string

// Analyzer is the single analyzer the vettool runs. Its plan names the
// analyzer a rule uses and that analyzer's params.
var Analyzer = &analysis.Analyzer{
	Name: "formwork",
	Doc:  "formwork go/types rules; the -plan flag names the rule this run evaluates",
	Run:  run,
}

func init() {
	Analyzer.Flags.StringVar(&planFlag, "plan", "", "the rule to evaluate, as JSON")
}

// plan is one rule's request: the analyzer it uses and that analyzer's
// params, decoded strictly by the analyzer.
type plan struct {
	Analyzer string          `json:"analyzer"`
	Params   json.RawMessage `json:"params"`
}

// check is one decoded rule, ready to run over a package.
type check interface {
	run(pass *analysis.Pass)
}

// analyzers decode a plan's params into a check. Each key is the rule type's
// name less its "go/" prefix.
var analyzers = map[string]func(json.RawMessage) (check, error){
	"forbidden-type-in-position": newForbiddenType,
}

func decodePlan() (check, error) {
	var p plan
	if planFlag == "" {
		return nil, errors.New("formwork vettool: no -formwork.plan given")
	}
	if err := strictJSON([]byte(planFlag), &p); err != nil {
		return nil, fmt.Errorf("formwork vettool: plan: %w", err)
	}
	decode, ok := analyzers[p.Analyzer]
	if !ok {
		return nil, fmt.Errorf("formwork vettool: plan names unknown analyzer %q", p.Analyzer)
	}
	c, err := decode(p.Params)
	if err != nil {
		return nil, fmt.Errorf("formwork vettool: %s: %w", p.Analyzer, err)
	}
	return c, nil
}

func strictJSON(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func run(pass *analysis.Pass) (any, error) {
	c, err := decodePlan()
	if err != nil {
		return nil, err
	}
	c.run(pass)
	return nil, nil
}

// Main runs the analyzers as a `go vet` vettool and does not return.
func Main() {
	unitchecker.Main(Analyzer)
}
