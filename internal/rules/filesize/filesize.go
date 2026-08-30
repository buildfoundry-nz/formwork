// Package filesize implements the `file-size` rule type: per-file line-count
// caps. A default cap applies to every in-scope file; glob overrides adjust it
// (first match wins) and an optional hard cap is an absolute ceiling that binds
// regardless of overrides. A cap of 0 means unlimited.
package filesize

import (
	"errors"
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type params struct {
	Cap       int             `yaml:"cap"`
	HardCap   int             `yaml:"hard_cap"`
	Overrides []overrideParam `yaml:"overrides"`
}

type overrideParam struct {
	Glob string `yaml:"glob"`
	Cap  int    `yaml:"cap"`
}

type override struct {
	glob string
	cap  int
}

type filesize struct {
	cap       int // default cap; 0 = unlimited
	hardCap   int // absolute ceiling; 0 = none
	overrides []override
}

func newFileSize(paramsNode *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(paramsNode, &p); err != nil {
		return nil, err
	}
	if p.Cap < 0 {
		return nil, fmt.Errorf("file-size: cap must not be negative, got %d", p.Cap)
	}
	if p.HardCap < 0 {
		return nil, fmt.Errorf("file-size: hard_cap must not be negative, got %d", p.HardCap)
	}
	c := &filesize{cap: p.Cap, hardCap: p.HardCap}
	for i, o := range p.Overrides {
		if o.Glob == "" {
			return nil, fmt.Errorf("file-size: overrides[%d].glob is required", i)
		}
		if !doublestar.ValidatePattern(o.Glob) {
			return nil, fmt.Errorf("file-size: overrides[%d].glob %q is not a valid glob", i, o.Glob)
		}
		if o.Cap < 0 {
			return nil, fmt.Errorf("file-size: overrides[%d].cap must not be negative, got %d", i, o.Cap)
		}
		c.overrides = append(c.overrides, override{glob: o.Glob, cap: o.Cap})
	}
	if c.cap == 0 && c.hardCap == 0 && len(c.overrides) == 0 {
		return nil, errors.New("file-size: at least one of cap, hard_cap, or overrides must be set")
	}
	return c, nil
}

func (c *filesize) CheckFile(f *scan.File) ([]rules.Match, error) {
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	n := len(lines)
	cap, label := c.effectiveCap(f.Path())
	if cap == 0 || n <= cap {
		return nil, nil
	}
	return []rules.Match{{
		Message: fmt.Sprintf("file has %d lines, exceeds %s of %d", n, label, cap),
	}}, nil
}

// effectiveCap resolves the binding cap for path and a label naming it. The
// applicable cap is the first matching override else the default; a positive
// hard cap then clamps it (and supplies the cap when the applicable one is
// unlimited). A returned cap of 0 means unlimited.
func (c *filesize) effectiveCap(path string) (int, string) {
	cap := c.cap
	for _, o := range c.overrides {
		if ok, _ := doublestar.Match(o.glob, path); ok {
			cap = o.cap
			break
		}
	}
	label := "cap"
	if c.hardCap > 0 && (cap == 0 || c.hardCap < cap) {
		cap = c.hardCap
		label = "hard cap"
	}
	return cap, label
}

func init() {
	rules.Register("file-size", newFileSize)
}
