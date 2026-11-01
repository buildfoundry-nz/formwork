package sqlextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// template_index.go — every way the walk instantiates each SQL template.
//
// A template's body is SQL at its definition as much as at its calls: a rule
// scoped to the package that writes a predicate expects to judge the
// predicate, wherever it is spliced. So the definition is judged once per
// distinct set of arguments the tree calls it with, and its body is a site
// only when some call passes an argument that is not a constant — then the
// definition cannot be judged whole, and neither can that call.
//
// The index is built once per walk (scan.FileSet.Memo through constEnv): every
// Go file the walk kept is parsed and each call of a template recorded, with
// its arguments folded. A call inside another template's body takes that
// template's parameters, so it is instantiated from the outer template's
// argument sets until nothing new appears.

type templateKey struct{ path, name string }

// templateUse is how the walk calls one template.
type templateUse struct {
	argSets []map[string]string
	seen    map[string]bool
	dynamic bool // some call passes an argument that is not a constant
}

func (u *templateUse) add(set map[string]string) bool {
	keys := make([]string, 0, len(set))
	for k, v := range set {
		keys = append(keys, k+"\x00"+v)
	}
	sort.Strings(keys)
	id := strings.Join(keys, "\x01")
	if u.seen[id] {
		return false
	}
	u.seen[id] = true
	u.argSets = append(u.argSets, set)
	return true
}

// nestedCall is a template call inside another template's body.
type nestedCall struct {
	outer templateKey
	call  *ast.CallExpr
	cf    *constFile
}

// useOf returns how the walk calls the template fd declared in cf, or nil
// when there is no walk to read.
func (e *constEnv) useOf(fd *ast.FuncDecl, cf *constFile) *templateUse {
	if e == nil {
		return nil
	}
	e.usesOnce.Do(e.buildUses)
	if u, ok := e.uses[templateKey{cf.path, fd.Name.Name}]; ok {
		return u
	}
	return &templateUse{seen: map[string]bool{}}
}

func (e *constEnv) buildUses() {
	e.uses = map[templateKey]*templateUse{}
	use := func(k templateKey) *templateUse {
		u := e.uses[k]
		if u == nil {
			u = &templateUse{seen: map[string]bool{}}
			e.uses[k] = u
		}
		return u
	}
	var nested []nestedCall
	for _, f := range e.tree.Files {
		p := f.Path()
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		src, err := f.Content()
		if err != nil || !strings.Contains(string(src), "(") {
			continue
		}
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, p, src, 0)
		if err != nil {
			continue
		}
		cf := newConstFile(p, file)
		type body struct {
			key    templateKey
			lo, hi token.Pos
		}
		var bodies []body
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && isTemplate(fd) {
				b := fd.Body.List[0].(*ast.ReturnStmt).Results[0]
				bodies = append(bodies, body{templateKey{p, fd.Name.Name}, b.Pos(), b.End()})
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis != token.NoPos {
				return true
			}
			fd, fcf := e.templateFunc(call.Fun, cf)
			if fd == nil {
				return true
			}
			k := templateKey{fcf.path, fd.Name.Name}
			set, all, ok := e.bindings(fd, call.Args, cf, nil, 0)
			if ok && all {
				use(k).add(set) // constant wherever it stands
				return true
			}
			for _, b := range bodies {
				if call.Pos() >= b.lo && call.End() <= b.hi {
					// Takes the enclosing template's parameters.
					nested = append(nested, nestedCall{outer: b.key, call: call, cf: cf})
					return true
				}
			}
			use(k).dynamic = true
			return true
		})
	}
	// Instantiate calls inside template bodies from the outer template's
	// argument sets, to a fixed point. An outer template some call passes a
	// non-constant makes every template it calls dynamic too.
	for changed := true; changed; {
		changed = false
		for _, nc := range nested {
			outer := e.uses[nc.outer]
			fd, fcf := e.templateFunc(nc.call.Fun, nc.cf)
			if outer == nil || fd == nil {
				continue
			}
			u := use(templateKey{fcf.path, fd.Name.Name})
			if outer.dynamic && !u.dynamic {
				u.dynamic, changed = true, true
			}
			for _, env := range outer.argSets {
				set, all, ok := e.bindings(fd, nc.call.Args, nc.cf, env, 0)
				if !ok || !all {
					if !u.dynamic {
						u.dynamic, changed = true, true
					}
					continue
				}
				if u.add(set) {
					changed = true
				}
			}
		}
	}
}
