// Package filenaming implements the `file-naming` rule type (spec §5): naming
// constraints over in-scope file paths (never their content). It is a fast,
// per-file Checker keyed on the repo-relative path.
package filenaming

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type fileNamingParams struct {
	ForbidExt    []string `yaml:"forbid_ext"`
	RequireMatch string   `yaml:"require_match"`
	Reserved     []string `yaml:"reserved"`
}

type fileNaming struct {
	forbidExt    map[string]bool // e.g. ".foo": a path with this extension is a violation
	requireMatch *regexp.Regexp  // a path not matching this is a violation
	reserved     []string        // globs; a path matching any is a violation
}

func newFileNaming(params *yaml.Node) (rules.Checker, error) {
	var p fileNamingParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	if len(p.ForbidExt) == 0 && p.RequireMatch == "" && len(p.Reserved) == 0 {
		return nil, errors.New("file-naming: at least one of forbid_ext, require_match, reserved must be set")
	}
	c := &fileNaming{}
	if len(p.ForbidExt) > 0 {
		c.forbidExt = make(map[string]bool, len(p.ForbidExt))
		for _, e := range p.ForbidExt {
			if !strings.HasPrefix(e, ".") || e == "." {
				return nil, fmt.Errorf("file-naming: forbid_ext entries must be extensions like %q, got %q", ".foo", e)
			}
			c.forbidExt[e] = true
		}
	}
	if p.RequireMatch != "" {
		re, err := regexp.Compile(p.RequireMatch)
		if err != nil {
			return nil, fmt.Errorf("file-naming: invalid require_match: %w", err)
		}
		c.requireMatch = re
	}
	if len(p.Reserved) > 0 {
		for _, g := range p.Reserved {
			if !doublestar.ValidatePattern(g) {
				return nil, fmt.Errorf("file-naming: invalid reserved glob %q", g)
			}
		}
		c.reserved = p.Reserved
	}
	return c, nil
}

// CheckFile emits one finding per rule the path violates, in a fixed order
// (forbid_ext, require_match, reserved). Findings leave Path empty; the engine
// fills in the file's path.
func (c *fileNaming) CheckFile(f *scan.File) ([]rules.Match, error) {
	p := f.Path()
	var matches []rules.Match
	if c.forbidExt != nil {
		if ext := path.Ext(p); c.forbidExt[ext] {
			matches = append(matches, rules.Match{Message: "forbidden file extension: " + ext})
		}
	}
	if c.requireMatch != nil && !c.requireMatch.MatchString(p) {
		matches = append(matches, rules.Match{Message: "file path does not match required pattern: " + c.requireMatch.String()})
	}
	for _, g := range c.reserved {
		if ok, _ := doublestar.Match(g, p); ok {
			matches = append(matches, rules.Match{Message: "reserved file path matched glob: " + g})
			break
		}
	}
	return matches, nil
}

func init() {
	rules.Register("file-naming", newFileNaming)
}
