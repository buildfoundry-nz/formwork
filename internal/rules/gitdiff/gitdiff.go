// Package gitdiff implements the `git-diff` rule type (spec §5): assertions
// over the added/removed lines of a git range — "no new X", "don't delete Y".
// It is a heavy, whole-run rule; a git failure is an engine error (exit 2),
// never a silent pass (spec §11).
package gitdiff

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/vcs"
	"gopkg.in/yaml.v3"
)

type params struct {
	Range         string `yaml:"range"`
	ForbidAdded   string `yaml:"forbid_added"`
	ForbidRemoved string `yaml:"forbid_removed"`
}

type gitdiff struct {
	rng           string
	forbidAdded   *regexp.Regexp
	forbidRemoved *regexp.Regexp
}

func newGitDiff(node *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Range) == "" {
		return nil, errors.New("git-diff: params.range is required (e.g. origin/main..HEAD)")
	}
	if p.ForbidAdded == "" && p.ForbidRemoved == "" {
		return nil, errors.New("git-diff: at least one of forbid_added / forbid_removed is required")
	}
	g := &gitdiff{rng: p.Range}
	var err error
	if p.ForbidAdded != "" {
		if g.forbidAdded, err = regexp.Compile(p.ForbidAdded); err != nil {
			return nil, fmt.Errorf("git-diff: invalid forbid_added: %w", err)
		}
	}
	if p.ForbidRemoved != "" {
		if g.forbidRemoved, err = regexp.Compile(p.ForbidRemoved); err != nil {
			return nil, fmt.Errorf("git-diff: invalid forbid_removed: %w", err)
		}
	}
	return g, nil
}

// Cost marks git-diff heavy (spec §8): it shells out to git.
func (*gitdiff) Cost() rules.Cost { return rules.CostHeavy }

// ProcessBound is false: git is not a multi-GB analyzer. CostHeavy still
// drops it from --skip-escapes; the engine runs it at full --workers.
func (*gitdiff) ProcessBound() bool { return false }

// CheckFile is a no-op: git-diff does not consume the scan (spec §6).
func (*gitdiff) CheckFile(*scan.File) ([]rules.Match, error) { return nil, nil }

// FinalizeErr diffs the configured range at the repo root and reports every
// added line matching forbid_added and every removed line matching
// forbid_removed. A git failure is an engine error.
func (c *gitdiff) FinalizeErr(ctx rules.FinalizeContext) ([]rules.Match, error) {
	diff, err := vcs.Diff(ctx.Root, c.rng)
	if err != nil {
		return nil, err
	}
	var matches []rules.Match
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if c.forbidAdded != nil {
				content := line[1:]
				if c.forbidAdded.MatchString(content) {
					matches = append(matches, rules.Match{Message: fmt.Sprintf("forbidden added line in %s: %q", c.rng, trim(content))})
				}
			}
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			if c.forbidRemoved != nil {
				content := line[1:]
				if c.forbidRemoved.MatchString(content) {
					matches = append(matches, rules.Match{Message: fmt.Sprintf("forbidden removed line in %s: %q", c.rng, trim(content))})
				}
			}
		}
	}
	return matches, nil
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func init() {
	rules.Register("git-diff", newGitDiff)
}
