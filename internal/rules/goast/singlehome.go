package goast

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// go/marked-single-home holds every top-level declaration whose doc comment
// carries the marker to one declaration across the rule's scope. The marker is
// the declaration of the home: a second func, method, type, const or var of
// the same identity anywhere in scope is a fork of it, marked or not.
//
// It is the abstract form of a family of pattern-count rules that each pinned
// one name to n: 1. Those asserted a home one rule at a time; this one rule
// asserts every home the source declares, so a new home is a comment at the
// declaration rather than another rule, fixture pair and mutation proof.
//
// Identity is the declaration kind plus its name, and a method's receiver type
// as well, so a var named like a marked func, or another type's method of the
// same name, is a different declaration. Unmarked duplicates are not this
// rule's business. A marker that documents no top-level declaration declares
// nothing, and is reported, because a home it was meant to hold is unheld.
type singleHomeParams struct {
	Marker string `yaml:"marker"`
}

type declSite struct {
	path string
	line int
}

type markedSingleHome struct {
	marker string

	mu    sync.Mutex
	decls map[string][]declSite // identity → every declaration in scope
	homes map[string][]declSite // identity → the declarations carrying the marker
	stray []rules.Match         // markers documenting no top-level declaration
}

func newMarkedSingleHome(node *yaml.Node) (rules.Checker, error) {
	var p singleHomeParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Marker) == "" {
		return nil, fmt.Errorf("go/marked-single-home: params.marker is required")
	}
	return &markedSingleHome{marker: p.Marker, decls: map[string][]declSite{}, homes: map[string][]declSite{}}, nil
}

// CheckFile records every top-level declaration of f and which carry the
// marker. Every verdict waits for Finalize: a fork may sit in a file read
// before its home.
func (c *markedSingleHome) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGoWithComments(f)
	if err != nil || !ok {
		return nil, err
	}
	type found struct {
		id     string
		site   declSite
		marked bool
	}
	var got []found
	docs := map[*ast.CommentGroup]bool{}
	add := func(id string, pos token.Pos, doc *ast.CommentGroup) {
		docs[doc] = true
		got = append(got, found{id, declSite{f.Path(), fset.Position(pos).Line}, c.carries(doc)})
	}
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			add(funcIdentity(x), x.Pos(), x.Doc)
		case *ast.GenDecl:
			for _, spec := range x.Specs {
				doc := specDoc(x, spec)
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add("type "+s.Name.Name, s.Pos(), doc)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						add(x.Tok.String()+" "+n.Name, n.Pos(), doc)
					}
				}
			}
		}
	}
	var stray []rules.Match
	for _, cg := range file.Comments {
		if !docs[cg] && c.carries(cg) {
			stray = append(stray, rules.Match{
				Path: f.Path(), Line: fset.Position(cg.Pos()).Line,
				Message: fmt.Sprintf("%q documents no top-level declaration, so it holds no home: put it in the doc comment of the func, method, type, const or var it declares single-home", c.marker),
			})
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range got {
		c.decls[g.id] = append(c.decls[g.id], g.site)
		if g.marked {
			c.homes[g.id] = append(c.homes[g.id], g.site)
		}
	}
	c.stray = append(c.stray, stray...)
	return nil, nil
}

func (c *markedSingleHome) carries(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, cm := range doc.List {
		if strings.Contains(cm.Text, c.marker) {
			return true
		}
	}
	return false
}

// specDoc is the comment documenting one spec: its own, or the declaration's
// when the declaration holds that spec alone. A doc on a grouped block covers
// none of its specs, since it cannot say which of them it declares.
func specDoc(d *ast.GenDecl, spec ast.Spec) *ast.CommentGroup {
	var own *ast.CommentGroup
	switch s := spec.(type) {
	case *ast.TypeSpec:
		own = s.Doc
	case *ast.ValueSpec:
		own = s.Doc
	}
	if own == nil && len(d.Specs) == 1 {
		return d.Doc
	}
	return own
}

// funcIdentity is "func name", or "method Recv.name" for a method, the
// receiver type stripped of pointer and type parameters.
func funcIdentity(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return "func " + d.Name.Name
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		case *ast.ParenExpr:
			t = x.X
			continue
		}
		break
	}
	name := "?"
	if id, ok := t.(*ast.Ident); ok {
		name = id.Name
	}
	return "method " + name + "." + d.Name.Name
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
			msg := fmt.Sprintf("%s is declared single-home at %s:%d; this second declaration forks it. Call or extend the home instead", id, home.path, home.line)
			if len(homes) > 1 {
				msg = fmt.Sprintf("%s carries %q in %d declarations; one declaration is its home", id, c.marker, len(homes))
			}
			if s == home && len(homes) == 1 {
				continue
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
	rules.Register("go/marked-single-home", newMarkedSingleHome)
}
