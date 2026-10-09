// Package pattern implements the line-oriented pattern rule types
// (spec §5: forbidden-pattern, required-pattern).
package pattern

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type forbiddenParams struct {
	Pattern string   `yaml:"pattern"`
	AllOf   []string `yaml:"all_of"` // co-occurrence: violate iff EVERY pattern appears in the file
	// NoneOf is all_of mode's EXCUSAL list, with require_absent's shape and rules.
	NoneOf         []excusalSpec `yaml:"none_of"`
	RequirePresent []string      `yaml:"require_present"` // file-level guard on `pattern`: report only if the file contains ALL of these
	// RequireAbsent is pattern mode's EXCUSAL list: credentials and evidence,
	// see excusalSpec for both and for why a bare string is refused.
	RequireAbsent []excusalSpec `yaml:"require_absent"`
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
	Prefilter string   `yaml:"prefilter"`
	Syntax    string   `yaml:"syntax"`    // "" | re2 | regexp2
	Multiline bool     `yaml:"multiline"` // match over whole file content, not line-by-line
	DeniedBy  []string `yaml:"denied_by"` // suppress a match whose text DENIES the topic it matched (#4)
	Window    int      `yaml:"window"`    // all_of only: patterns must co-occur within this many consecutive lines (0 = whole file)
}

type forbidden struct {
	re             lineMatcher
	allOf          []lineMatcher // whole-file co-occurrence (RE2, linear — no backtracking)
	requirePresent []lineMatcher // whole-file guards on `pattern` mode (RE2, linear)
	ex             excusals
	prefilter      string // if set, a file not containing this literal cannot match — skip cheaply
	multiline      bool
	denial         *denial // nil unless params.denied_by is set (#4)
	window         int     // all_of only: 0 = whole-file co-occurrence; >0 = within this many consecutive lines

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
	if len(p.DeniedBy) > 0 && !hasPattern {
		// all_of is a whole-file co-occurrence with no single match position,
		// so there is no "text immediately before the match" to read. Rejecting
		// it is the honest answer; accepting it would silently do nothing.
		return nil, errors.New("forbidden-pattern: denied_by applies to params.pattern, not params.all_of")
	}
	den, err := newDenial(p.DeniedBy)
	if err != nil {
		return nil, fmt.Errorf("forbidden-pattern: %w", err)
	}
	if p.Window > 0 && !hasAllOf {
		return nil, errors.New("forbidden-pattern: window applies to params.all_of")
	}
	c := &forbidden{
		denial:            den,
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
		ex, err := compileExcusals("none_of", p.NoneOf, p.Syntax)
		if err != nil {
			return nil, err
		}
		c.ex = ex
		return withCredentials(c), nil
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
	ex, err := compileExcusals("require_absent", p.RequireAbsent, p.Syntax)
	if err != nil {
		return nil, err
	}
	c.ex = ex
	return withCredentials(c), nil
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

// fileGuardsFail reports whether the whole-file require_present guards reject
// this file (so the line-anchored pattern must not be reported). Credentials
// (require_absent) are decided per line by holdsCredential, not here.
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
	return false, nil
}

func (c *forbidden) CheckFile(f *scan.File) ([]rules.Match, error) {
	// Co-occurrence mode: cheap linear scans over whole content.
	if len(c.allOf) > 0 {
		held, err := c.ex.holdsCredential(f)
		if err != nil {
			return nil, err
		}
		if held {
			return nil, nil // a none_of credential is held → excused, and counted
		}
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		s := string(content)
		if c.prefilter != "" && !strings.Contains(s, c.prefilter) {
			return nil, nil
		}
		if c.window > 0 {
			ms, err := c.windowedAllOf(f)
			if err != nil || len(ms) == 0 {
				return ms, err
			}
			return c.unlessEvidenced(s, ms[0].Line, ms)
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
		ms := []rules.Match{{Line: 1, Message: "forbidden co-occurrence matched (all_of present)"}}
		if len(c.ex.evidence) == 0 {
			return ms, nil
		}
		// Evidence is measured from where the first all_of pattern first matches.
		first, err := matchStartLines(c.allOf[0], s)
		if err != nil || len(first) == 0 {
			return ms, err
		}
		return c.unlessEvidenced(s, first[0], ms)
	}
	// multiline: match the pattern against the whole (preprocessed) file so a
	// cross-line block/co-occurrence can match (spec §5). Reports one finding at
	// the line where the first match starts.
	if c.multiline {
		held, err := c.ex.holdsCredential(f)
		if err != nil {
			return nil, err
		}
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		s := string(content)
		if c.prefilter != "" && !strings.Contains(s, c.prefilter) {
			return nil, nil // cheap literal gate — skip the backtracking regex
		}
		if held {
			return nil, nil
		}
		fail, err := c.fileGuardsFail(s)
		if err != nil {
			return nil, err
		}
		if fail {
			return nil, nil
		}
		if len(c.ex.evidence) > 0 {
			// Every trigger must be discharged by evidence near it; the first
			// one that is not is the finding.
			idx, err := c.ex.indexEvidence(s)
			if err != nil {
				return nil, err
			}
			triggers, err := matchStartLines(c.re, s)
			if err != nil {
				return nil, err
			}
			for _, tl := range triggers {
				if !c.ex.excused(idx, tl) {
					return []rules.Match{{Line: tl, Message: "forbidden pattern matched (multiline): " + c.re.String()}}, nil
				}
			}
			return nil, nil
		}
		line, ok, err := c.re.FindLine(s)
		if err != nil {
			return nil, err
		}
		if ok && c.denial != nil {
			idx, found, ferr := c.re.FindIndex(s)
			if ferr != nil {
				return nil, ferr
			}
			if found && c.denial.deniedAt(s, idx) {
				return nil, nil
			}
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
	// Credentials are counted BEFORE the prefilter gate: a holder in a file the
	// prefilter skips is still a holder, and an uncounted holder is exactly the
	// forgery the count exists to catch.
	held, err := c.ex.holdsCredential(f)
	if err != nil {
		return nil, err
	}
	if held {
		return nil, nil
	}
	guarded := len(c.requirePresent) > 0 || len(c.ex.credentials) > 0 || len(c.ex.evidence) > 0
	if c.prefilter != "" || len(c.requirePresent) > 0 {
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		s := string(content)
		if c.prefilter != "" && !strings.Contains(s, c.prefilter) {
			return nil, nil
		}
		if len(c.requirePresent) > 0 {
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
	var evIdx evidenceIndex
	if len(c.ex.evidence) > 0 {
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		if evIdx, err = c.ex.indexEvidence(string(content)); err != nil {
			return nil, err
		}
	}
	var matches []rules.Match
	for i, line := range lines {
		ok, err := c.re.MatchString(line)
		if err != nil {
			return nil, err
		}
		if ok && evIdx != nil && c.ex.excused(evIdx, i+1) {
			continue // this trigger is discharged by evidence beside it
		}
		if ok && c.denial != nil {
			// Second stage: drop a match whose text DENIES the topic it matched
			// (#4). The position comes from the matcher rather than a search, so
			// the prefix handed over is the real text before the match; an
			// unlocatable position leaves ok true and the finding stands.
			idx, found, err := c.re.FindIndex(line)
			if err != nil {
				return nil, err
			}
			if found && c.denial.deniedAt(line, idx) {
				continue
			}
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

// unlessEvidenced drops ms when evidence sits within distance of anchor.
func (c *forbidden) unlessEvidenced(s string, anchor int, ms []rules.Match) ([]rules.Match, error) {
	if len(c.ex.evidence) == 0 {
		return ms, nil
	}
	idx, err := c.ex.indexEvidence(s)
	if err != nil {
		return nil, err
	}
	if c.ex.excused(idx, anchor) {
		return nil, nil
	}
	return ms, nil
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
	// Fresh holder counters: the copy is evaluated as a separate run, and a
	// shared counter would add its holders to the original's.
	cp.ex.credentials = make([]*credential, len(c.ex.credentials))
	for i, cr := range c.ex.credentials {
		fresh := *cr
		fresh.seen = &atomic.Int64{}
		cp.ex.credentials[i] = &fresh
	}
	return &cp
}

func init() {
	rules.Register("forbidden-pattern", newForbidden)
}
