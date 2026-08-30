// Package patterncount implements the `pattern-count` rule type (spec §5): a
// whole-scope assertion on how many lines match a regex across the in-scope
// file set. CheckFile tallies matching lines per file into a shared total;
// Finalize compares that total to n under one of exactly | at-most | at-least
// and reports a single scope-level finding on violation.
package patterncount

import (
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type params struct {
	Pattern string `yaml:"pattern"`
	Op      string `yaml:"op"`
	N       *int   `yaml:"n"`
}

const (
	opExactly = "exactly"
	opAtMost  = "at-most"
	opAtLeast = "at-least"
)

type patternCount struct {
	re *regexp.Regexp
	op string
	n  int64

	total atomic.Int64 // matching lines observed across all in-scope files
}

func newPatternCount(node *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("pattern-count: params.pattern is required")
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("pattern-count: invalid pattern: %w", err)
	}
	switch p.Op {
	case "":
		return nil, errors.New("pattern-count: params.op is required")
	case opExactly, opAtMost, opAtLeast:
	default:
		return nil, fmt.Errorf("pattern-count: unknown op %q (want %q, %q, or %q)", p.Op, opExactly, opAtMost, opAtLeast)
	}
	if p.N == nil {
		return nil, errors.New("pattern-count: params.n is required")
	}
	if *p.N < 0 {
		return nil, fmt.Errorf("pattern-count: params.n must be >= 0, got %d", *p.N)
	}
	return &patternCount{re: re, op: p.Op, n: int64(*p.N)}, nil
}

// CheckFile counts the file's matching lines and adds them to the running
// total. It emits no per-file findings; the verdict is scope-level. Safe for
// concurrent use: the tally is atomic.
func (c *patternCount) CheckFile(f *scan.File) ([]rules.Match, error) {
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	var n int64
	for _, line := range lines {
		if c.re.MatchString(line) {
			n++
		}
	}
	if n > 0 {
		c.total.Add(n)
	}
	return nil, nil
}

// Finalize compares the accumulated match total to n under the configured op
// and returns a single scope-level finding on violation.
func (c *patternCount) Finalize() []rules.Match {
	total := c.total.Load()
	var bad bool
	switch c.op {
	case opExactly:
		bad = total != c.n
	case opAtMost:
		bad = total > c.n
	case opAtLeast:
		bad = total < c.n
	}
	if !bad {
		return nil
	}
	return []rules.Match{{
		Message: fmt.Sprintf("pattern %q matched %d line(s) across scope, want %s %d", c.re.String(), total, c.op, c.n),
	}}
}

// WholeTreeInvariant is always true: the verdict compares a scope-wide match
// total to n, so a changeset scan tallies only the changed files and reports
// the wrong count (#4).
func (c *patternCount) WholeTreeInvariant() bool { return true }

func init() {
	rules.Register("pattern-count", newPatternCount)
}
