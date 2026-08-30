// Package setrelation implements the `set-relation` rule type (spec §5): a
// cross-file join that extracts a string set from each of two file groups and
// asserts a relation (subset, equal, disjoint) between them.
package setrelation

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type sideParams struct {
	Files   []string `yaml:"files"`
	Pattern string   `yaml:"pattern"`
	Group   *int     `yaml:"group"`
	// MinCount, when set, is the minimum cardinality the extracted set must
	// reach before Finalize may accept the relation. Default 0 keeps empty∩empty
	// green for back-compat; equal|subset test-claim rules should set ≥1 so a
	// zero-evidence join cannot pass (the validating port's vacuity class V4).
	MinCount *int `yaml:"min_count"`
	// Preprocess is an optional per-side content transform applied after the
	// rule-level preprocess the engine already ran. Falls back to no extra
	// transform when empty. Lets A and B sit on different planes (e.g. B on
	// code-only-dart while A stays raw).
	Preprocess string `yaml:"preprocess"`
}

type params struct {
	A        sideParams `yaml:"a"`
	B        sideParams `yaml:"b"`
	Relation string     `yaml:"relation"`
}

const (
	relSubset   = "subset"
	relEqual    = "equal"
	relDisjoint = "disjoint"
)

// side collects the string set extracted from one file group. Its values map
// is written from CheckFile, which the engine runs concurrently, so mu guards it.
type side struct {
	globs      []string
	re         *regexp.Regexp
	group      int
	minCount   int
	preprocess string
	mu         sync.Mutex
	values     map[string]bool
}

type setRelation struct {
	a, b     *side
	relation string
}

func newSide(label string, p sideParams) (*side, error) {
	if len(p.Files) == 0 {
		return nil, fmt.Errorf("set-relation: %s.files is required", label)
	}
	for _, g := range p.Files {
		if !doublestar.ValidatePattern(g) {
			return nil, fmt.Errorf("set-relation: %s.files: invalid glob %q", label, g)
		}
	}
	if p.Pattern == "" {
		return nil, fmt.Errorf("set-relation: %s.pattern is required", label)
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return nil, fmt.Errorf("set-relation: %s.pattern: invalid regex: %w", label, err)
	}
	group := 1
	if p.Group != nil {
		group = *p.Group
	}
	if group < 0 || group > re.NumSubexp() {
		return nil, fmt.Errorf("set-relation: %s.group %d out of range (pattern has %d capture group(s))", label, group, re.NumSubexp())
	}
	minCount := 0
	if p.MinCount != nil {
		minCount = *p.MinCount
		if minCount < 0 {
			return nil, fmt.Errorf("set-relation: %s.min_count must be ≥ 0, got %d", label, minCount)
		}
	}
	if p.Preprocess != "" {
		if _, ok := preprocess.Lookup(p.Preprocess); !ok {
			return nil, fmt.Errorf("set-relation: %s.preprocess: unknown preprocessor %q (known: %v)",
				label, p.Preprocess, preprocess.Names())
		}
	}
	return &side{
		globs:      p.Files,
		re:         re,
		group:      group,
		minCount:   minCount,
		preprocess: p.Preprocess,
		values:     map[string]bool{},
	}, nil
}

func newSetRelation(node *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	switch p.Relation {
	case relSubset, relEqual, relDisjoint:
	case "":
		return nil, errors.New("set-relation: params.relation is required")
	default:
		return nil, fmt.Errorf("set-relation: unknown relation %q (want %q, %q, or %q)", p.Relation, relSubset, relEqual, relDisjoint)
	}
	a, err := newSide("a", p.A)
	if err != nil {
		return nil, err
	}
	b, err := newSide("b", p.B)
	if err != nil {
		return nil, err
	}
	return &setRelation{a: a, b: b, relation: p.Relation}, nil
}

// CheckFile classifies the file into either group by glob and extracts every
// capture-group value into that group's set. It emits no per-file findings; the
// relation is asserted once in Finalize. A file matching both groups' globs
// contributes to both sets. I/O or preprocess failures are engine errors (exit 2),
// never a silent empty side that would green a subset (I16).
func (c *setRelation) CheckFile(f *scan.File) ([]rules.Match, error) {
	if err := c.a.collect(f); err != nil {
		return nil, err
	}
	if err := c.b.collect(f); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *side) collect(f *scan.File) error {
	if !matchAny(s.globs, f.Path()) {
		return nil
	}
	work := f
	if s.preprocess != "" {
		v, err := f.Variant(s.preprocess)
		if err != nil {
			return err
		}
		work = v
	}
	lines, err := work.Lines()
	if err != nil {
		return err
	}
	for _, line := range lines {
		for _, m := range s.re.FindAllStringSubmatch(line, -1) {
			if s.group < len(m) {
				s.mu.Lock()
				s.values[m[s.group]] = true
				s.mu.Unlock()
			}
		}
	}
	return nil
}

func matchAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, path); ok {
			return true
		}
	}
	return false
}

// Finalize asserts the configured relation over the two collected sets,
// returning a single scope-level Match naming the sorted offending elements on
// violation. min_count floors are checked first so an empty join cannot pass
// equal/subset when a side demanded evidence.
func (c *setRelation) Finalize() []rules.Match {
	if ms := c.minCountFindings(); len(ms) > 0 {
		return ms
	}
	switch c.relation {
	case relSubset:
		if extra := difference(c.a.values, c.b.values); len(extra) > 0 {
			return scopeMatch("set-relation subset violated: elements in A missing from B: " + strings.Join(extra, ", "))
		}
	case relEqual:
		onlyA := difference(c.a.values, c.b.values)
		onlyB := difference(c.b.values, c.a.values)
		if len(onlyA) > 0 || len(onlyB) > 0 {
			var parts []string
			if len(onlyA) > 0 {
				parts = append(parts, "only in A: "+strings.Join(onlyA, ", "))
			}
			if len(onlyB) > 0 {
				parts = append(parts, "only in B: "+strings.Join(onlyB, ", "))
			}
			return scopeMatch("set-relation equal violated: " + strings.Join(parts, "; "))
		}
	case relDisjoint:
		if both := intersection(c.a.values, c.b.values); len(both) > 0 {
			return scopeMatch("set-relation disjoint violated: elements in both A and B: " + strings.Join(both, ", "))
		}
	}
	return nil
}

func (c *setRelation) minCountFindings() []rules.Match {
	// Report every short side so a dual min_count:1 equal rule names both A and B
	// when both are empty, rather than only the first.
	var parts []string
	if n := len(c.a.values); n < c.a.minCount {
		parts = append(parts, fmt.Sprintf("side A has %d element(s), min_count requires ≥%d", n, c.a.minCount))
	}
	if n := len(c.b.values); n < c.b.minCount {
		parts = append(parts, fmt.Sprintf("side B has %d element(s), min_count requires ≥%d", n, c.b.minCount))
	}
	if len(parts) == 0 {
		return nil
	}
	return scopeMatch("set-relation min_count violated: " + strings.Join(parts, "; "))
}

func scopeMatch(msg string) []rules.Match {
	return []rules.Match{{Message: msg}}
}

// difference returns the sorted elements of a that are not in b.
func difference(a, b map[string]bool) []string {
	var out []string
	for v := range a {
		if !b[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// intersection returns the sorted elements present in both a and b.
func intersection(a, b map[string]bool) []string {
	var out []string
	for v := range a {
		if b[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// WholeTreeInvariant is always true: the verdict joins two file-derived sets
// across the whole scope, so restricting the input to a changeset drops
// elements from either side and false-reports the relation (#4).
func (c *setRelation) WholeTreeInvariant() bool { return true }

func init() {
	rules.Register("set-relation", newSetRelation)
}
