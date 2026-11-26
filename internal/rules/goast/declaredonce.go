package goast

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// go/declared-once holds each listed top-level declaration to one declaration
// of its identity across the rule's scope. The list names the file that owns
// each declaration and its identity; a second func, method, type, const or var
// of that identity anywhere in scope is a duplicate of it. A listed
// declaration its file does not declare is also a finding, so the list cannot
// go stale.
//
// Identity is the declaration kind plus its name, and a method's receiver type
// as well, so a var named like a listed func, or another type's method of the
// same name, is a different declaration. Duplicates of unlisted names are not
// this rule's business.
type declaredOnceParams struct {
	Declarations []declaredEntry `yaml:"declarations"`
}

// declaredEntry is one declaration the rule holds: the file that owns it and
// its identity, written as a finding writes it ("func Name", "method
// Recv.Name", "type Name", "const Name", "var Name").
type declaredEntry struct {
	Path string `yaml:"path"`
	Decl string `yaml:"decl"`
}

var goDeclIdentity = regexp.MustCompile(`^(?:(?:func|type|const|var) [A-Za-z_][A-Za-z0-9_]*|method [A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*)$`)

type declSite struct {
	path string
	line int
}

type declaredOnce struct {
	owners  map[string]string // identity → the file the list says owns it
	entries []declaredEntry

	mu    sync.Mutex
	decls map[string][]declSite // listed identity → every declaration of it in scope
}

func newDeclaredOnce(node *yaml.Node) (rules.Checker, error) {
	var p declaredOnceParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if len(p.Declarations) == 0 {
		return nil, fmt.Errorf("go/declared-once: params.declarations is required")
	}
	c := &declaredOnce{owners: map[string]string{}, decls: map[string][]declSite{}}
	for i, d := range p.Declarations {
		if strings.TrimSpace(d.Path) == "" || d.Path != strings.TrimSpace(d.Path) {
			return nil, fmt.Errorf("go/declared-once: params.declarations[%d].path must be a repository path", i)
		}
		if !goDeclIdentity.MatchString(d.Decl) {
			return nil, fmt.Errorf("go/declared-once: params.declarations[%d].decl %q is not \"func Name\", \"method Recv.Name\", \"type Name\", \"const Name\" or \"var Name\"", i, d.Decl)
		}
		if owner, ok := c.owners[d.Decl]; ok {
			return nil, fmt.Errorf("go/declared-once: params.declarations lists %s twice (%s and %s); an identity has one owner", d.Decl, owner, d.Path)
		}
		c.owners[d.Decl] = d.Path
		c.entries = append(c.entries, d)
	}
	return c, nil
}

// CheckFile records every top-level declaration of f whose identity is
// listed. Every verdict waits for Finalize: a duplicate may sit in a file read
// before the owner.
func (c *declaredOnce) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGo(f)
	if err != nil || !ok {
		return nil, err
	}
	var got []struct {
		id   string
		site declSite
	}
	add := func(id string, pos token.Pos) {
		if _, listed := c.owners[id]; listed {
			got = append(got, struct {
				id   string
				site declSite
			}{id, declSite{f.Path(), fset.Position(pos).Line}})
		}
	}
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			add(funcIdentity(x), x.Pos())
		case *ast.GenDecl:
			for _, spec := range x.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add("type "+s.Name.Name, s.Pos())
				case *ast.ValueSpec:
					for _, n := range s.Names {
						add(x.Tok.String()+" "+n.Name, n.Pos())
					}
				}
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range got {
		c.decls[g.id] = append(c.decls[g.id], g.site)
	}
	return nil, nil
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

// Finalize reports every declaration of a listed identity other than its
// owner's, and every listed declaration its owner file does not declare, in
// path then line order.
func (c *declaredOnce) Finalize() []rules.Match {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []rules.Match
	for _, e := range c.entries {
		var owned *declSite
		for i, s := range c.decls[e.Decl] {
			if s.path == e.Path {
				owned = &c.decls[e.Decl][i]
				break
			}
		}
		if owned == nil {
			out = append(out, rules.Match{Path: e.Path, Line: 1,
				Message: fmt.Sprintf("%s is listed as declared once in %s, but that file declares no top-level %s; list the file that declares it", e.Decl, e.Path, e.Decl)})
			for _, s := range c.decls[e.Decl] {
				out = append(out, rules.Match{Path: s.path, Line: s.line,
					Message: fmt.Sprintf("%s is listed as declared once in %s, not here", e.Decl, e.Path)})
			}
			continue
		}
		for _, s := range c.decls[e.Decl] {
			if s == *owned {
				continue
			}
			out = append(out, rules.Match{Path: s.path, Line: s.line,
				Message: fmt.Sprintf("%s is declared once, at %s:%d; this second declaration duplicates it. Call or extend that declaration instead", e.Decl, owned.path, owned.line)})
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
	rules.Register("go/declared-once", newDeclaredOnce)
}
