// Package ordering implements the `ordering` rule type (spec §5): within each
// file, a `before` pattern must precede an `after` pattern. When both patterns
// are present, an `after` match on an earlier line than the first `before`
// match is a violation.
package ordering

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type orderingParams struct {
	Before string `yaml:"before"`
	After  string `yaml:"after"`
	Within string `yaml:"within"`
}

// withinFile is the only supported scope for now.
const withinFile = "file"

type ordering struct {
	before *regexp.Regexp
	after  *regexp.Regexp
}

func newOrdering(params *yaml.Node) (rules.Checker, error) {
	var p orderingParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Before == "" {
		return nil, errors.New("ordering: params.before is required")
	}
	if p.After == "" {
		return nil, errors.New("ordering: params.after is required")
	}
	within := p.Within
	if within == "" {
		within = withinFile
	}
	if within != withinFile {
		return nil, fmt.Errorf("ordering: unsupported within %q (want %q)", p.Within, withinFile)
	}
	before, err := regexp.Compile(p.Before)
	if err != nil {
		return nil, fmt.Errorf("ordering: invalid before: %w", err)
	}
	after, err := regexp.Compile(p.After)
	if err != nil {
		return nil, fmt.Errorf("ordering: invalid after: %w", err)
	}
	return &ordering{before: before, after: after}, nil
}

// CheckFile flags the first `after` line when it precedes the first `before`
// line. It reports nothing unless both patterns are present in the file.
func (c *ordering) CheckFile(f *scan.File) ([]rules.Match, error) {
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	beforeLine, afterLine := 0, 0
	for i, line := range lines {
		if beforeLine == 0 && c.before.MatchString(line) {
			beforeLine = i + 1
		}
		if afterLine == 0 && c.after.MatchString(line) {
			afterLine = i + 1
		}
		if beforeLine != 0 && afterLine != 0 {
			break
		}
	}
	if beforeLine == 0 || afterLine == 0 {
		return nil, nil
	}
	if afterLine < beforeLine {
		return []rules.Match{{
			Line:    afterLine,
			Message: fmt.Sprintf("after pattern %q precedes before pattern %q", c.after.String(), c.before.String()),
		}}, nil
	}
	return nil, nil
}

func init() {
	rules.Register("ordering", newOrdering)
}
