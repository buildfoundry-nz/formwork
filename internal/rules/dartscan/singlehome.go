package dartscan

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// dart/marked-single-home is go/marked-single-home for Dart: a top-level
// declaration whose comment run carries the marker is the one declaration of
// its identity across the scope, and any second declaration of it, marked or
// not, is a finding naming the home.
//
// A top-level declaration is a line at column 0 that starts outside a string
// and outside a block comment: dart format puts every top-level declaration
// there and indents every member, so a method or a static field named like a
// marked declaration is never counted. Strings and block comments are tracked
// across lines, so a `class` line inside a ”' string declares nothing.
//
// Identity is the kind (type, function, getter, setter, variable) and the
// name. A library-private name (`_Body`) is also keyed on its file, since two
// libraries may each declare their own. The marker sits in the `//` or `///`
// comment run directly above the declaration, past any annotations. A marker
// that run does not carry onto a top-level declaration is itself a finding.
type singleHomeParams struct {
	Marker string `yaml:"marker"`
}

type homeSite struct {
	path string
	line int
}

type markedSingleHome struct {
	marker string

	mu    sync.Mutex
	decls map[string][]homeSite
	homes map[string][]homeSite
	names map[string]string // identity → how a finding names it
	stray []rules.Match
}

func newMarkedSingleHome(node *yaml.Node) (rules.Checker, error) {
	var p singleHomeParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Marker) == "" {
		return nil, fmt.Errorf("dart/marked-single-home: params.marker is required")
	}
	return &markedSingleHome{marker: p.Marker, decls: map[string][]homeSite{}, homes: map[string][]homeSite{}, names: map[string]string{}}, nil
}

const dartName = `[A-Za-z_$][\w$]*`

var (
	declType      = regexp.MustCompile(`^(?:(?:abstract|sealed|final|base|interface|mixin)\s+)*(?:class|mixin|enum|typedef)\s+(` + dartName + `)`)
	declExtType   = regexp.MustCompile(`^extension\s+type\s+(?:const\s+)?(` + dartName + `)`)
	declExtension = regexp.MustCompile(`^extension\s+(` + dartName + `)\s*(?:<[^>]*>)?\s+on\b`)
	declGetter    = regexp.MustCompile(`^(?:[\w$<>?,.\[\]]+\s+)*get\s+(` + dartName + `)`)
	declSetter    = regexp.MustCompile(`^(?:[\w$<>?,.\[\]]+\s+)*set\s+(` + dartName + `)\s*\(`)
	declKeyVar    = regexp.MustCompile(`^(?:external\s+)?(?:late\s+)?(?:const|final|var)\s+(?:[\w$<>?,.\s]+?\s+)?(` + dartName + `)\s*[=;]`)
	declFunc      = regexp.MustCompile(`^(?:[\w$<>?,.\[\]]+\s+)*(` + dartName + `)\s*(?:<[^()]*>)?\s*\(`)
	declTypedVar  = regexp.MustCompile(`^(?:[\w$<>?,.\[\]]+\s+)+(` + dartName + `)\s*=`)
	notDeclStart  = regexp.MustCompile(`^(?:import|export|part|library)\b`)
)

// topLevelIdentity is the kind and name a column-0 line declares, or ok=false.
func topLevelIdentity(line string) (kind, name string, ok bool) {
	if notDeclStart.MatchString(line) {
		return "", "", false
	}
	for _, d := range []struct {
		kind string
		re   *regexp.Regexp
	}{
		{"type", declType}, {"type", declExtType}, {"type", declExtension},
		{"getter", declGetter}, {"setter", declSetter}, {"variable", declKeyVar},
		{"function", declFunc}, {"variable", declTypedVar},
	} {
		if m := d.re.FindStringSubmatch(line); m != nil {
			return d.kind, m[1], true
		}
	}
	return "", "", false
}

// lexState carries what a line ends inside of into the next one.
type lexState struct {
	block int    // nested /* */ depth
	quote string // the open ''' or """ delimiter, when inside one
}

func (s lexState) outside() bool { return s.block == 0 && s.quote == "" }

// advance moves s over one line and reports whether a `//` comment opens on it
// outside every string and block comment, with its column.
func (s *lexState) advance(line string) (commentAt int) {
	commentAt = -1
	for i := 0; i < len(line); {
		switch {
		case s.block > 0:
			if strings.HasPrefix(line[i:], "/*") {
				s.block++
				i += 2
			} else if strings.HasPrefix(line[i:], "*/") {
				s.block--
				i += 2
			} else {
				i++
			}
		case s.quote != "":
			if line[i] == '\\' {
				i += 2
			} else if strings.HasPrefix(line[i:], s.quote) {
				i += 3
				s.quote = ""
			} else {
				i++
			}
		case strings.HasPrefix(line[i:], "//"):
			return i
		case strings.HasPrefix(line[i:], "/*"):
			s.block++
			i += 2
		case strings.HasPrefix(line[i:], "'''") || strings.HasPrefix(line[i:], `"""`):
			s.quote = line[i : i+3]
			i += 3
		case line[i] == '\'' || line[i] == '"':
			q := line[i]
			i++
			for i < len(line) && line[i] != q {
				if line[i] == '\\' {
					i++
				}
				i++
			}
			i++
		default:
			i++
		}
	}
	return commentAt
}

// CheckFile records f's top-level declarations and which carry the marker.
// Every verdict waits for Finalize: a fork may sit in a file read first.
func (c *markedSingleHome) CheckFile(f *scan.File) ([]rules.Match, error) {
	if !isDart(f) {
		return nil, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	type found struct {
		id, label string
		site      homeSite
		marked    bool
	}
	var got []found
	var markers []int // 0-based lines whose own `//` comment carries the marker
	attached := map[int]bool{}
	var st lexState
	for i, line := range lines {
		startsOutside := st.outside()
		at := st.advance(line)
		if !startsOutside {
			continue
		}
		if at >= 0 && strings.Contains(line[at:], c.marker) && strings.TrimSpace(line[:at]) == "" {
			markers = append(markers, i)
		}
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		kind, name, ok := topLevelIdentity(line)
		if !ok {
			continue
		}
		id := kind + " " + name
		if strings.HasPrefix(name, "_") {
			id += "@" + f.Path()
		}
		marked := false
		for j := i - 1; j >= 0; j-- {
			t := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "@") {
				break
			}
			if strings.HasPrefix(t, "//") && strings.Contains(t, c.marker) {
				marked = true
				attached[j] = true
			}
		}
		got = append(got, found{id, kind + " " + name, homeSite{f.Path(), i + 1}, marked})
	}
	var stray []rules.Match
	for _, i := range markers {
		if !attached[i] {
			stray = append(stray, rules.Match{Path: f.Path(), Line: i + 1,
				Message: fmt.Sprintf("%q is on no top-level declaration, so it holds no home: put it in the comment run directly above the class, function, getter or variable it declares single-home", c.marker)})
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range got {
		c.decls[g.id] = append(c.decls[g.id], g.site)
		c.names[g.id] = g.label
		if g.marked {
			c.homes[g.id] = append(c.homes[g.id], g.site)
		}
	}
	c.stray = append(c.stray, stray...)
	return nil, nil
}

// Finalize reports every declaration that shares a marked identity with its
// home, and every stray marker, in path then line order.
func (c *markedSingleHome) Finalize() []rules.Match {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]rules.Match(nil), c.stray...)
	for id, homes := range c.homes {
		sites := c.decls[id]
		if len(sites) < 2 {
			continue
		}
		home := homes[0]
		for _, s := range sites {
			if s == home && len(homes) == 1 {
				continue
			}
			msg := fmt.Sprintf("%s is declared single-home at %s:%d; this second declaration forks it. Use or extend the home instead", c.names[id], home.path, home.line)
			if len(homes) > 1 {
				msg = fmt.Sprintf("%s carries %q in %d declarations; one declaration is its home", c.names[id], c.marker, len(homes))
			}
			out = append(out, rules.Match{Path: s.path, Line: s.line, Message: msg})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// WholeTreeInvariant is true: a fork in any in-scope file breaks a home in
// another, so the rule judges the tracked tree, never a changeset alone.
func (c *markedSingleHome) WholeTreeInvariant() bool { return true }

// ScopeBound is true: every declaration it counts comes from an in-scope file
// through CheckFile.
func (c *markedSingleHome) ScopeBound() bool { return true }

func init() {
	rules.Register("dart/marked-single-home", newMarkedSingleHome)
}
