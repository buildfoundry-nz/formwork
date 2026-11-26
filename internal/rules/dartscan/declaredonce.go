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

// dart/declared-once is go/declared-once for Dart: each listed top-level
// declaration is the one declaration of its identity across the scope, and
// any second declaration of it is a finding naming the owner. A listed
// declaration its owner file does not declare is a finding too.
//
// A top-level declaration is a line at column 0 that starts outside a string
// and outside a block comment: dart format puts every top-level declaration
// there and indents every member, so a method or a static field named like a
// listed declaration is never counted. Strings and block comments are tracked
// across lines, so a `class` line inside a ”' string declares nothing.
//
// Identity is the kind (type, function, getter, setter, variable) and the
// name. A library-private name (`_Body`) is also keyed on its file, since two
// libraries may each declare their own.
type declaredOnceParams struct {
	Declarations []declaredEntry `yaml:"declarations"`
}

// declaredEntry is one declaration the rule holds: the file that owns it and
// its identity, written as a finding writes it ("type Name", "function Name",
// "getter Name", "setter Name", "variable Name").
type declaredEntry struct {
	Path string `yaml:"path"`
	Decl string `yaml:"decl"`
}

var dartDeclIdentity = regexp.MustCompile(`^(?:type|function|getter|setter|variable) ` + dartName + `$`)

type declSite struct {
	path string
	line int
}

type declaredOnce struct {
	owners  map[string]string // keyed identity → the file the list says owns it
	entries []declaredEntry

	mu    sync.Mutex
	decls map[string][]declSite // listed keyed identity → every declaration of it
}

// keyed is the identity a declaration is counted under: a private name is
// also keyed on its file.
func keyed(decl, path string) string {
	if _, name, _ := strings.Cut(decl, " "); strings.HasPrefix(name, "_") {
		return decl + "@" + path
	}
	return decl
}

func newDeclaredOnce(node *yaml.Node) (rules.Checker, error) {
	var p declaredOnceParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if len(p.Declarations) == 0 {
		return nil, fmt.Errorf("dart/declared-once: params.declarations is required")
	}
	c := &declaredOnce{owners: map[string]string{}, decls: map[string][]declSite{}}
	for i, d := range p.Declarations {
		if strings.TrimSpace(d.Path) == "" || d.Path != strings.TrimSpace(d.Path) {
			return nil, fmt.Errorf("dart/declared-once: params.declarations[%d].path must be a repository path", i)
		}
		if !dartDeclIdentity.MatchString(d.Decl) {
			return nil, fmt.Errorf("dart/declared-once: params.declarations[%d].decl %q is not \"type Name\", \"function Name\", \"getter Name\", \"setter Name\" or \"variable Name\"", i, d.Decl)
		}
		k := keyed(d.Decl, d.Path)
		if owner, ok := c.owners[k]; ok {
			return nil, fmt.Errorf("dart/declared-once: params.declarations lists %s twice (%s and %s); an identity has one owner", d.Decl, owner, d.Path)
		}
		c.owners[k] = d.Path
		c.entries = append(c.entries, d)
	}
	return c, nil
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

// CheckFile records every top-level declaration of f whose identity is
// listed. Every verdict waits for Finalize: a duplicate may sit in a file read
// before the owner.
func (c *declaredOnce) CheckFile(f *scan.File) ([]rules.Match, error) {
	if !isDart(f) {
		return nil, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	var got []struct {
		id   string
		site declSite
	}
	var st lexState
	for i, line := range strings.Split(string(content), "\n") {
		startsOutside := st.outside()
		st.advance(line)
		if !startsOutside || line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		kind, name, ok := topLevelIdentity(line)
		if !ok {
			continue
		}
		id := keyed(kind+" "+name, f.Path())
		if _, listed := c.owners[id]; listed {
			got = append(got, struct {
				id   string
				site declSite
			}{id, declSite{f.Path(), i + 1}})
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range got {
		c.decls[g.id] = append(c.decls[g.id], g.site)
	}
	return nil, nil
}

// Finalize reports every declaration of a listed identity other than its
// owner's, and every listed declaration its owner file does not declare, in
// path then line order.
func (c *declaredOnce) Finalize() []rules.Match {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []rules.Match
	for _, e := range c.entries {
		k := keyed(e.Decl, e.Path)
		var owned *declSite
		for i, s := range c.decls[k] {
			if s.path == e.Path {
				owned = &c.decls[k][i]
				break
			}
		}
		if owned == nil {
			out = append(out, rules.Match{Path: e.Path, Line: 1,
				Message: fmt.Sprintf("%s is listed as declared once in %s, but that file declares no top-level %s; list the file that declares it", e.Decl, e.Path, e.Decl)})
			for _, s := range c.decls[k] {
				out = append(out, rules.Match{Path: s.path, Line: s.line,
					Message: fmt.Sprintf("%s is listed as declared once in %s, not here", e.Decl, e.Path)})
			}
			continue
		}
		for _, s := range c.decls[k] {
			if s == *owned {
				continue
			}
			out = append(out, rules.Match{Path: s.path, Line: s.line,
				Message: fmt.Sprintf("%s is declared once, at %s:%d; this second declaration duplicates it. Use or extend that declaration instead", e.Decl, owned.path, owned.line)})
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

// WholeTreeInvariant is true: a duplicate in any in-scope file breaks a
// declaration owned by another, so the rule judges the tracked tree, never a
// changeset alone.
func (c *declaredOnce) WholeTreeInvariant() bool { return true }

// ScopeBound is true: every declaration it counts comes from an in-scope file
// through CheckFile.
func (c *declaredOnce) ScopeBound() bool { return true }

func init() {
	rules.Register("dart/declared-once", newDeclaredOnce)
}
