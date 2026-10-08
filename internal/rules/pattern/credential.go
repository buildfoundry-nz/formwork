package pattern

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// require_absent (pattern mode) and none_of (all_of mode) list EXCUSALS: a
// trigger the rule would report is let off. Before 0.9 an entry was a bare
// regex matched anywhere in the file, which made every excusal an exemption
// any file could claim:
//
//   - COPY: the entry's text inside a string, or in any line at all, excused
//     the whole file, because nothing tied the match to what it was meant to
//     prove;
//   - GROWTH: nothing bounded how many files held it, so a new file opted
//     itself out by carrying one;
//   - DISTANCE: an entry anywhere in the file excused a trigger hundreds of
//     lines away that it had nothing to do with.
//
// An excusal is now one of exactly two things, and says which.
//
// A CREDENTIAL names an owner: "this file is the declaration, so it may spell
// what everyone else must not". It is a line pattern anchored at the start of
// the line, and it declares `holders`, the exact number of lines in the rule's
// scope that match it. A run whose count differs is a finding, so the set of
// excused files is fixed and every change to it is a reviewed edit to a number.
//
// EVIDENCE names a discharge: "this trigger is handled, and here is the proof".
// It excuses only a trigger it occurs within `within` lines of (before or
// after), capped at maxEvidenceWithin. Evidence elsewhere in the file excuses
// nothing, so it cannot be used as a file-wide opt-out. A rule whose proof is
// genuinely file-wide ("a file containing X must contain shape Y") is a
// requirement, not an excusal: write it as required-pattern with `when:`.
//
// A bare string is refused at load, as is any entry that is not exactly one of
// the two forms.
type excusalSpec struct {
	Credential string `yaml:"credential"`
	Holders    *int   `yaml:"holders"`
	Evidence   string `yaml:"evidence"`
	Within     *int   `yaml:"within"`
}

// maxEvidenceWithin bounds how far evidence may sit from its trigger. Past this
// the proof is no longer about the trigger; it is a property of the file, and
// required-pattern with `when:` says so honestly.
const maxEvidenceWithin = 100

func (s *excusalSpec) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return errors.New("require_absent and none_of entries are {credential, holders} or {evidence, within}, not a bare string: " +
			"a bare entry excuses any file that contains its text anywhere")
	}
	type plain excusalSpec
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*s = excusalSpec(p)
	return nil
}

// lineAnchored accepts `^...` and an inline flag group followed by `^`, such
// as `(?i)^`. Anything else can match mid-line, which is the copy forgery.
var lineAnchored = regexp.MustCompile(`^(\(\?[a-zA-Z]+\))?\^`)

type credential struct {
	m    lineMatcher
	src  string
	want int64
	seen *atomic.Int64
}

type evidence struct {
	m      lineMatcher
	src    string
	within int
}

// excusals is a rule's compiled require_absent or none_of list.
type excusals struct {
	credentials []*credential
	evidence    []*evidence
}

func compileExcusals(key string, specs []excusalSpec, syntax string) (excusals, error) {
	var out excusals
	for i, s := range specs {
		where := fmt.Sprintf("forbidden-pattern %s[%d]", key, i)
		isCred, isEv := s.Credential != "" || s.Holders != nil, s.Evidence != "" || s.Within != nil
		switch {
		case isCred && isEv:
			return out, fmt.Errorf("%s: an entry is a credential or evidence, not both", where)
		case isCred:
			if s.Credential == "" {
				return out, fmt.Errorf("%s: credential is required", where)
			}
			if !lineAnchored.MatchString(s.Credential) {
				return out, fmt.Errorf("%s: credential %q must be anchored at the start of a line (^...): "+
					"an unanchored credential is earned by any line that merely contains its text", where, s.Credential)
			}
			if s.Holders == nil {
				return out, fmt.Errorf("%s: holders is required: the exact number of lines in scope that hold %q", where, s.Credential)
			}
			if *s.Holders < 1 {
				return out, fmt.Errorf("%s: holders must be >= 1, got %d: a credential no line holds excuses nothing; delete it", where, *s.Holders)
			}
			m, err := compileMatcher(where, s.Credential, syntax)
			if err != nil {
				return out, err
			}
			out.credentials = append(out.credentials, &credential{m: m, src: s.Credential, want: int64(*s.Holders), seen: &atomic.Int64{}})
		case isEv:
			if s.Evidence == "" {
				return out, fmt.Errorf("%s: evidence is required", where)
			}
			if s.Within == nil {
				return out, fmt.Errorf("%s: within is required: evidence excuses only a trigger it sits within that many lines of", where)
			}
			if *s.Within < 0 || *s.Within > maxEvidenceWithin {
				return out, fmt.Errorf("%s: within must be between 0 and %d, got %d: evidence further away proves something about the file, "+
					"not the trigger; write the rule as required-pattern with when:", where, maxEvidenceWithin, *s.Within)
			}
			m, err := compileMatcher(where, s.Evidence, syntax)
			if err != nil {
				return out, err
			}
			out.evidence = append(out.evidence, &evidence{m: m, src: s.Evidence, within: *s.Within})
		default:
			return out, fmt.Errorf("%s: set credential+holders or evidence+within", where)
		}
	}
	return out, nil
}

// holdsCredential counts every credential line in f and reports whether f
// holds at least one. It runs on every in-scope file before any prefilter, so
// the count is over the whole scope.
func (x excusals) holdsCredential(f *scan.File) (bool, error) {
	if len(x.credentials) == 0 {
		return false, nil
	}
	lines, err := f.Lines()
	if err != nil {
		return false, err
	}
	held := false
	for _, cr := range x.credentials {
		var n int64
		for _, line := range lines {
			ok, err := cr.m.MatchString(line)
			if err != nil {
				return false, err
			}
			if ok {
				n++
			}
		}
		if n > 0 {
			cr.seen.Add(n)
			held = true
		}
	}
	return held, nil
}

// evidenceIndex holds, per evidence entry, the 1-based lines where a match
// STARTS. Evidence is matched over the whole content so a multi-line proof
// works; only its start line is used for distance.
type evidenceIndex [][]int

func (x excusals) indexEvidence(content string) (evidenceIndex, error) {
	if len(x.evidence) == 0 {
		return nil, nil
	}
	idx := make(evidenceIndex, len(x.evidence))
	for i, ev := range x.evidence {
		lines, err := matchStartLines(ev.m, content)
		if err != nil {
			return nil, err
		}
		idx[i] = lines
	}
	return idx, nil
}

// excused reports whether some evidence entry starts within its distance of
// the trigger at line.
func (x excusals) excused(idx evidenceIndex, line int) bool {
	for i, ev := range x.evidence {
		lines := idx[i]
		j := sort.SearchInts(lines, line-ev.within)
		if j < len(lines) && lines[j] <= line+ev.within {
			return true
		}
	}
	return false
}

// matchStartLines returns the 1-based start line of the first match on each
// line of s, in order.
func matchStartLines(m lineMatcher, s string) ([]int, error) {
	// Only the start LINE matters, so after each match the search resumes at
	// the next line: one match per line is all distance needs.
	var out []int
	off, line, counted := 0, 1, 0
	for off < len(s) {
		rel, ok, err := m.FindIndex(s[off:])
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		start := off + rel
		for ; counted < start; counted++ {
			if s[counted] == '\n' {
				line++
			}
		}
		out = append(out, line)
		nl := strings.IndexByte(s[start:], '\n')
		if nl < 0 {
			break
		}
		off = start + nl + 1
	}
	return out, nil
}

// counted is a forbidden-pattern that carries credentials. Only it is a
// Finalizer and a whole-tree invariant: a rule without credentials keeps the
// per-file, range-scopeable shape every consumer (census, --staged) relies on.
type counted struct{ *forbidden }

// withCredentials wraps c when it holds credentials.
func withCredentials(c *forbidden) rules.Checker {
	if len(c.ex.credentials) == 0 {
		return c
	}
	return counted{c}
}

// Finalize reports every credential whose holder count differs from its
// declaration. A credential excuses each file that holds it, so a holder the
// declaration does not account for is a file that excused itself.
func (c counted) Finalize() []rules.Match {
	var out []rules.Match
	for _, cr := range c.ex.credentials {
		if got := cr.seen.Load(); got != cr.want {
			out = append(out, rules.Match{
				Message: fmt.Sprintf("credential %q is held by %d line(s) across scope, declared holders %d: "+
					"a credential excuses every file that holds it, so its holders are fixed", cr.src, got, cr.want),
			})
		}
	}
	return out
}

// WholeTreeInvariant: the holder count is scope-wide, which a changeset scan
// cannot judge.
func (c counted) WholeTreeInvariant() bool { return true }

// HasCredentials lets the config loader refuse a credential on a library rule:
// its holder count is a fact about the consuming repo.
func (c counted) HasCredentials() bool { return true }

// WithoutPrefilter keeps the wrapper, with fresh counters.
func (c counted) WithoutPrefilter() rules.Checker {
	return withCredentials(c.forbidden.WithoutPrefilter().(*forbidden))
}
