// Package pattern implements the line-oriented pattern rule types
// (spec §5: forbidden-pattern, required-pattern).
package pattern

import (
	"errors"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type forbiddenParams struct {
	Pattern        string   `yaml:"pattern"`
	AllOf          []string `yaml:"all_of"`          // co-occurrence: violate iff EVERY pattern appears in the file
	NoneOf         []string `yaml:"none_of"`         // ...and (with all_of) none of these appear
	RequirePresent []string `yaml:"require_present"` // file-level guard on `pattern`: report only if the file contains ALL of these
	RequireAbsent  []string `yaml:"require_absent"`  // file-level guard on `pattern`: report only if the file contains NONE of these
	// Prefilter is a pure optimization: a cheap literal gate that must never
	// change the verdict. formwork lint (prefilter-load-bearing) rejects a
	// load-bearing prefilter; put semantic scope in require_present: instead.
	//
	// The literal must be one that EVERY string the pattern can match
	// necessarily contains. On an alternation that means every branch: a
	// literal shared by only some of them silently kills the rest, and on a
	// tombstone rule nothing on the tree will ever reveal it. lint proves this
	// from the rule's fixtures and from the pattern itself (implication.go),
	// and reports a prefilter it cannot prove either way as unproven (#133).
	Prefilter string `yaml:"prefilter"`
	Syntax    string `yaml:"syntax"`    // "" | re2 | regexp2
	Multiline bool   `yaml:"multiline"` // match over whole file content, not line-by-line
	Window    int    `yaml:"window"`    // all_of only: patterns must co-occur within this many consecutive lines (0 = whole file)
}

type forbidden struct {
	re             lineMatcher
	allOf          []lineMatcher // whole-file co-occurrence (RE2, linear — no backtracking)
	noneOf         []lineMatcher
	requirePresent []lineMatcher // whole-file guards on `pattern` mode (RE2, linear)
	requireAbsent  []lineMatcher
	prefilter      string // if set, a file not containing this literal cannot match — skip cheaply
	multiline      bool
	window         int // all_of only: 0 = whole-file co-occurrence; >0 = within this many consecutive lines

	// Pattern sources kept verbatim for the prefilter-implication analysis
	// (implication.go): it re-parses them with regexp/syntax to decide whether
	// a match is possible without the prefilter literal. lineMatcher.String()
	// round-trips the source but loses which backend compiled it, and the
	// analysis is only valid for the RE2 syntaxes — hence syntax too.
	src               string   // plain `pattern` mode
	allOfSrc          []string // all_of mode
	requirePresentSrc []string // whole-file guards — conjuncts with src, so they can imply the prefilter too
	syntax            string
}

func newForbidden(params *yaml.Node) (rules.Checker, error) {
	var p forbiddenParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	hasPattern, hasAllOf := p.Pattern != "", len(p.AllOf) > 0
	if hasPattern == hasAllOf {
		return nil, errors.New("forbidden-pattern: set exactly one of params.pattern or params.all_of")
	}
	hasGuards := len(p.RequirePresent) > 0 || len(p.RequireAbsent) > 0
	if hasGuards && !hasPattern {
		// all_of is itself whole-file co-occurrence; require_* only guards the
		// line-anchored `pattern` mode (where they preserve the trigger's line).
		return nil, errors.New("forbidden-pattern: require_present/require_absent apply to params.pattern, not params.all_of")
	}
	if p.Window < 0 {
		return nil, errors.New("forbidden-pattern: window must be >= 0")
	}
	if p.Window > 0 && !hasAllOf {
		return nil, errors.New("forbidden-pattern: window applies to params.all_of")
	}
	c := &forbidden{
		multiline:         p.Multiline,
		prefilter:         p.Prefilter,
		window:            p.Window,
		src:               p.Pattern,
		allOfSrc:          p.AllOf,
		requirePresentSrc: p.RequirePresent,
		syntax:            p.Syntax,
	}
	if hasAllOf {
		// Co-occurrence mode: a file violates when every all_of pattern is present
		// (and no none_of pattern is). Each is matched over whole content with the
		// linear RE2 engine — the fast replacement for a backtracking regexp2
		// lookahead conjunction like (?=[\s\S]*A)(?=[\s\S]*B)(?![\s\S]*C).
		for _, pat := range p.AllOf {
			m, err := compileMatcher("forbidden-pattern all_of", pat, p.Syntax)
			if err != nil {
				return nil, err
			}
			c.allOf = append(c.allOf, m)
		}
		for _, pat := range p.NoneOf {
			m, err := compileMatcher("forbidden-pattern none_of", pat, p.Syntax)
			if err != nil {
				return nil, err
			}
			c.noneOf = append(c.noneOf, m)
		}
		return c, nil
	}
	re, err := compileMatcher("forbidden-pattern", p.Pattern, p.Syntax)
	if err != nil {
		return nil, err
	}
	c.re = re
	for _, pat := range p.RequirePresent {
		m, err := compileMatcher("forbidden-pattern require_present", pat, p.Syntax)
		if err != nil {
			return nil, err
		}
		c.requirePresent = append(c.requirePresent, m)
	}
	for _, pat := range p.RequireAbsent {
		m, err := compileMatcher("forbidden-pattern require_absent", pat, p.Syntax)
		if err != nil {
			return nil, err
		}
		c.requireAbsent = append(c.requireAbsent, m)
	}
	return c, nil
}

// windowedAllOf fires when every all_of pattern matches within some sliding
// window of c.window consecutive lines. It collects each pattern's matching
// line numbers, then two-pointers over the merged, line-sorted match events to
// find the first N-line span holding at least one match from every pattern —
// O(lines · patterns), no backtracking. Anchors on the earliest line of that
// span. This is the linear form of the awk proximity window.
func (c *forbidden) windowedAllOf(f *scan.File) ([]rules.Match, error) {
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	type event struct {
		line, pat int
	}
	var events []event // built in ascending line order (outer loop is line-major)
	for i, line := range lines {
		for p, m := range c.allOf {
			ok, err := m.MatchString(line)
			if err != nil {
				return nil, err
			}
			if ok {
				events = append(events, event{i + 1, p})
			}
		}
	}
	k := len(c.allOf)
	count := make([]int, k)
	distinct, lo := 0, 0
	for hi := 0; hi < len(events); hi++ {
		if count[events[hi].pat] == 0 {
			distinct++
		}
		count[events[hi].pat]++
		for events[hi].line-events[lo].line >= c.window { // span outside the N-line window
			count[events[lo].pat]--
			if count[events[lo].pat] == 0 {
				distinct--
			}
			lo++
		}
		if distinct == k {
			return []rules.Match{{
				Line:    events[lo].line,
				Message: "forbidden co-occurrence matched (all_of within window)",
			}}, nil
		}
	}
	return nil, nil
}

// fileGuardsFail reports whether the whole-file require_present/require_absent
// guards reject this file (so the line-anchored pattern must not be reported).
// One linear scan per guard — the RE2 replacement for a backtracking lookaround.
func (c *forbidden) fileGuardsFail(s string) (bool, error) {
	for _, m := range c.requirePresent {
		ok, err := m.MatchString(s)
		if err != nil {
			return false, err
		}
		if !ok {
			return true, nil // a required-present pattern is absent
		}
	}
	for _, m := range c.requireAbsent {
		ok, err := m.MatchString(s)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil // a required-absent pattern is present
		}
	}
	return false, nil
}

func (c *forbidden) CheckFile(f *scan.File) ([]rules.Match, error) {
	// Co-occurrence mode: cheap linear scans over whole content.
	if len(c.allOf) > 0 {
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		s := string(content)
		if c.prefilter != "" && !strings.Contains(s, c.prefilter) {
			return nil, nil
		}
		for _, m := range c.noneOf {
			ok, err := m.MatchString(s)
			if err != nil {
				return nil, err
			}
			if ok {
				return nil, nil // an excluded pattern is present → not a violation
			}
		}
		if c.window > 0 {
			return c.windowedAllOf(f)
		}
		for _, m := range c.allOf {
			ok, err := m.MatchString(s)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil // a required pattern is absent → no co-occurrence
			}
		}
		return []rules.Match{{Line: 1, Message: "forbidden co-occurrence matched (all_of present)"}}, nil
	}
	// multiline: match the pattern against the whole (preprocessed) file so a
	// cross-line block/co-occurrence can match (spec §5). Reports one finding at
	// the line where the first match starts.
	if c.multiline {
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		s := string(content)
		if c.prefilter != "" && !strings.Contains(s, c.prefilter) {
			return nil, nil // cheap literal gate — skip the backtracking regex
		}
		fail, err := c.fileGuardsFail(s)
		if err != nil {
			return nil, err
		}
		if fail {
			return nil, nil
		}
		line, ok, err := c.re.FindLine(s)
		if err != nil {
			return nil, err
		}
		if ok {
			return []rules.Match{{Line: line, Message: "forbidden pattern matched (multiline): " + c.re.String()}}, nil
		}
		return nil, nil
	}
	// require_present/require_absent guard the line-anchored trigger: evaluate the
	// whole-file co-occurrence once (linear), skip the file if it fails. The
	// prefilter gate runs whether or not the rule is guarded — a plain
	// single-pattern rule honours it like every other mode (#21).
	guarded := len(c.requirePresent) > 0 || len(c.requireAbsent) > 0
	if c.prefilter != "" || guarded {
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		s := string(content)
		if c.prefilter != "" && !strings.Contains(s, c.prefilter) {
			return nil, nil
		}
		if guarded {
			fail, err := c.fileGuardsFail(s)
			if err != nil {
				return nil, err
			}
			if fail {
				return nil, nil
			}
		}
	}
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	var matches []rules.Match
	for i, line := range lines {
		ok, err := c.re.MatchString(line)
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, rules.Match{
				Line:    i + 1,
				Message: "forbidden pattern matched: " + c.re.String(),
			})
			if guarded {
				// A guarded pattern is a file-level co-occurrence assertion — one
				// finding per file, anchored on the first trigger (parity with the
				// origin's single grep -q). Unguarded patterns still flag every line.
				break
			}
		}
	}
	return matches, nil
}

// Prefilter reports the rule's literal prefilter gate ("" if none). Part of the
// rules.Prefiltered contract consumed by lint's load-bearing-prefilter check.
func (c *forbidden) Prefilter() string { return c.prefilter }

// WithoutPrefilter returns an equivalent checker with the prefilter gate
// removed. forbidden is stateless — its compiled matchers are safe for
// concurrent reuse (rules.Checker contract) — so a shallow copy sharing them is
// correct.
func (c *forbidden) WithoutPrefilter() rules.Checker {
	cp := *c
	cp.prefilter = ""
	return &cp
}

func init() {
	rules.Register("forbidden-pattern", newForbidden)
}
