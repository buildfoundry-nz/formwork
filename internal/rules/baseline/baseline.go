// Package baseline implements the `baseline` rule type (spec §5): it extracts a
// set of values from the in-scope files and compares that set to a tracked
// baseline file. Mode `exact` requires the two sets to be equal (additions AND
// removals are findings); mode `shrink-only` is a ratchet that permits removals
// but reports additions (values present now but absent from the baseline). It
// needs the repository root to read the baseline file, so it is an
// ErrFinalizer: a missing baseline file is an engine error (exit 2), never a
// silent pass (spec §11). It stays fast cost — only tool/git rules are heavy.
package baseline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

const (
	modeExact      = "exact"
	modeShrinkOnly = "shrink-only"
)

type params struct {
	Pattern  string `yaml:"pattern"`
	Group    *int   `yaml:"group"` // pointer so an explicit 0 differs from unset (default 1)
	Baseline string `yaml:"baseline"`
	Mode     string `yaml:"mode"`
}

type baseline struct {
	re       *regexp.Regexp
	group    int
	baseline string // repo-relative path to the tracked baseline file
	mode     string

	mu   sync.Mutex
	seen map[string]bool // set of extracted values across all in-scope files
}

func newBaseline(node *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("baseline: params.pattern is required")
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("baseline: invalid pattern: %w", err)
	}
	group := 1
	if p.Group != nil {
		group = *p.Group
	}
	if group < 0 {
		return nil, fmt.Errorf("baseline: group must be >= 0, got %d", group)
	}
	if group > re.NumSubexp() {
		return nil, fmt.Errorf("baseline: group %d out of range for pattern with %d capture group(s)", group, re.NumSubexp())
	}
	if strings.TrimSpace(p.Baseline) == "" {
		return nil, errors.New("baseline: params.baseline (path to the baseline file) is required")
	}
	if p.Mode != modeExact && p.Mode != modeShrinkOnly {
		return nil, fmt.Errorf("baseline: mode must be %q or %q, got %q", modeExact, modeShrinkOnly, p.Mode)
	}
	return &baseline{
		re:       re,
		group:    group,
		baseline: p.Baseline,
		mode:     p.Mode,
		seen:     map[string]bool{},
	}, nil
}

// CheckFile extracts every capture-group value on every line into the shared
// set. It is called concurrently, so the set is mutex-guarded. It emits no
// per-file findings — the comparison happens once, in FinalizeErr.
func (c *baseline) CheckFile(f *scan.File) ([]rules.Match, error) {
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	var found []string
	for _, line := range lines {
		for _, sub := range c.re.FindAllStringSubmatch(line, -1) {
			found = append(found, sub[c.group])
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	c.mu.Lock()
	for _, v := range found {
		c.seen[v] = true
	}
	c.mu.Unlock()
	return nil, nil
}

// FinalizeErr reads the baseline file at ctx.Root and compares it to the
// extracted set. A missing/unreadable baseline file is an engine error. Reported
// values are sorted for deterministic output; scope-level findings leave Path
// empty.
func (c *baseline) FinalizeErr(ctx rules.FinalizeContext) ([]rules.Match, error) {
	data, err := os.ReadFile(filepath.Join(ctx.Root, c.baseline))
	if err != nil {
		return nil, fmt.Errorf("baseline: cannot read baseline file %s: %w", c.baseline, err)
	}
	want := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		want[s] = true
	}

	c.mu.Lock()
	have := make(map[string]bool, len(c.seen))
	for v := range c.seen {
		have[v] = true
	}
	c.mu.Unlock()

	var matches []rules.Match

	// Additions — present now, absent from the baseline — are findings in both
	// modes (they are what the shrink-only ratchet forbids).
	additions := diff(have, want)
	for _, v := range additions {
		matches = append(matches, rules.Match{
			Message: fmt.Sprintf("value %q is present but not in baseline %s", v, c.baseline),
		})
	}

	// Removals — in the baseline but no longer present — are findings only in
	// exact mode; shrink-only permits them.
	if c.mode == modeExact {
		removals := diff(want, have)
		for _, v := range removals {
			matches = append(matches, rules.Match{
				Message: fmt.Sprintf("value %q is in baseline %s but no longer present (exact match required)", v, c.baseline),
			})
		}
	}

	return matches, nil
}

// diff returns the sorted elements of a not present in b.
func diff(a, b map[string]bool) []string {
	var out []string
	for v := range a {
		if !b[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// WholeTreeInvariant is always true: the verdict compares the whole scope's
// extracted set against a tracked baseline, so a changeset scan sees only the
// changed files and false-reports removals (exact) or misses additions
// (shrink-only) — #4.
func (c *baseline) WholeTreeInvariant() bool { return true }

func init() {
	rules.Register("baseline", newBaseline)
}
