// Package docpathexists implements the `doc-path-exists` rule type (spec §5):
// repo-relative paths cited in docs and comments must exist on disk. A
// capturing regex pulls each cited path token out of the in-scope files;
// existence is resolved once, after the whole scan, against the repository
// root. A cited path that is absent is a finding at its citing location; only
// an unexpected stat failure is an engine error (exit 2), never a silent pass
// (spec §11).
package docpathexists

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type params struct {
	Pattern string `yaml:"pattern"`
}

// citation is one path token cited by an in-scope file, kept with its origin
// so a missing target reports the citing file:line.
type citation struct {
	path  string
	line  int
	token string
}

type docPathExists struct {
	re *regexp.Regexp

	mu    sync.Mutex
	cites []citation
}

func newDocPathExists(node *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("doc-path-exists: params.pattern is required")
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("doc-path-exists: invalid pattern: %w", err)
	}
	if n := re.NumSubexp(); n != 1 {
		return nil, fmt.Errorf("doc-path-exists: pattern must have exactly one capturing group yielding a path token, has %d", n)
	}
	return &docPathExists{re: re}, nil
}

// CheckFile records every cited path token in f. It never emits per-file
// findings: existence is decided once, in FinalizeErr, against the repo root.
// It is safe for concurrent use — the shared citation slice is mutex-guarded.
func (c *docPathExists) CheckFile(f *scan.File) ([]rules.Match, error) {
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	var local []citation
	for i, line := range lines {
		for _, m := range c.re.FindAllStringSubmatch(line, -1) {
			token := m[1]
			if token == "" {
				continue
			}
			local = append(local, citation{path: f.Path(), line: i + 1, token: token})
		}
	}
	if len(local) == 0 {
		return nil, nil
	}
	c.mu.Lock()
	c.cites = append(c.cites, local...)
	c.mu.Unlock()
	return nil, nil
}

// FinalizeErr resolves each cited token against ctx.Root and reports the ones
// that do not exist, each at its citing file:line, sorted by (path, line,
// token) for deterministic output. A missing cited path is a finding; only an
// unexpected stat error is an engine error.
func (c *docPathExists) FinalizeErr(ctx rules.FinalizeContext) ([]rules.Match, error) {
	c.mu.Lock()
	cites := append([]citation(nil), c.cites...)
	c.mu.Unlock()

	sort.Slice(cites, func(i, j int) bool {
		if cites[i].path != cites[j].path {
			return cites[i].path < cites[j].path
		}
		if cites[i].line != cites[j].line {
			return cites[i].line < cites[j].line
		}
		return cites[i].token < cites[j].token
	})

	// A token can be cited many times; stat each distinct token once.
	seen := map[string]bool{}
	var matches []rules.Match
	for _, ci := range cites {
		ok, cached := seen[ci.token]
		if !cached {
			var err error
			ok, err = pathExists(filepath.Join(ctx.Root, filepath.FromSlash(ci.token)))
			if err != nil {
				return nil, fmt.Errorf("doc-path-exists: resolving %q cited in %s:%d: %w", ci.token, ci.path, ci.line, err)
			}
			seen[ci.token] = ok
		}
		if !ok {
			matches = append(matches, rules.Match{Path: ci.path, Line: ci.line, Message: "cited path does not exist: " + ci.token})
		}
	}
	return matches, nil
}

// pathExists reports whether p is present. A non-existence result is not an
// error; any other stat failure is.
func pathExists(p string) (bool, error) {
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func init() {
	rules.Register("doc-path-exists", newDocPathExists)
}
