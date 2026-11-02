package sqlextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
)

// inert_flow_fields.go — where an exported field's value can go.
//
// A value of a struct exists only in the package that declares it and the packages
// that import that one, directly or not. So a field's node belongs to its declaring
// package, and a selector in package Q reaches the fields of its name declared in Q
// or anywhere Q imports. No receiver type is known here, so one selector reaches
// every such field, which is the safe side of what it can mean.
//
// A write the module cannot follow is bad: a field no struct in reach declares, a
// literal or a receiver of a type from outside the tree, a package's exported
// variable, a generated message's field. Text there is out of the walk's sight.

// flowFields is what the graph knows of the module's struct fields and imports.
type flowFields struct {
	declared map[string][]string        // field name -> the directories declaring it
	own      map[string]map[string]bool // directory -> the fields it declares
	imports  map[string]map[string]bool // directory -> the directories it imports
	reach    map[string]map[string]bool // directory -> itself and all it imports
	nodes    map[[2]string][]string     // {directory, field} -> the nodes a selector reaches
}

func newFlowFields() *flowFields {
	return &flowFields{declared: map[string][]string{}, own: map[string]map[string]bool{}, imports: map[string]map[string]bool{},
		reach: map[string]map[string]bool{}, nodes: map[[2]string][]string{}}
}

func fieldNode(dir, name string) string { return "field " + dir + "." + name }

// declareFields records every struct field the file spells and the directories it imports.
func (ff *flowFile) declareFields() {
	fields := ff.g.fields
	fields.declareStructs(ff.file, ff.cf.dir)
	for _, is := range ff.file.Imports {
		ip, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		if dir := ff.e.importDir(ff.cf.dir, ip); dir != "" {
			if fields.imports[ff.cf.dir] == nil {
				fields.imports[ff.cf.dir] = map[string]bool{}
			}
			fields.imports[ff.cf.dir][dir] = true
		}
	}
}

func (f *flowFields) declareStructs(file *ast.File, dir string) {
	ast.Inspect(file, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				f.declare(dir, name.Name)
			}
			if len(field.Names) == 0 {
				if name := embeddedName(field.Type); name != "" {
					f.declare(dir, name)
				}
			}
		}
		return true
	})
}

func (f *flowFields) declare(dir, name string) {
	if f.own[dir] == nil {
		f.own[dir] = map[string]bool{}
	}
	if !f.own[dir][name] {
		f.own[dir][name] = true
		f.declared[name] = append(f.declared[name], dir)
	}
}

// declareGenerated records a generated file's fields, whose values leave on the wire.
func (g *flowGraph) declareGenerated(p string, src []byte) {
	file, err := parser.ParseFile(token.NewFileSet(), p, src, parser.SkipObjectResolution)
	if err != nil {
		return
	}
	dir := path.Dir(p)
	wire := newFlowFields()
	wire.declareStructs(file, dir)
	for name := range wire.own[dir] {
		g.fields.declare(dir, name)
		g.edge(fieldNode(dir, name), badFlow)
	}
}

func embeddedName(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	}
	return ""
}

// reachOf is dir and every directory it imports, directly or not.
func (f *flowFields) reachOf(dir string) map[string]bool {
	if r, ok := f.reach[dir]; ok {
		return r
	}
	r := map[string]bool{dir: true}
	queue := []string{dir}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		for to := range f.imports[d] {
			if !r[to] {
				r[to] = true
				queue = append(queue, to)
			}
		}
	}
	f.reach[dir] = r
	return r
}

// fieldNodes is the node of every field a selector of this name in this file can
// mean. An unexported field is its own package's. Outside any module the imports do
// not resolve, so an exported name reaches every declaration of it.
func (ff *flowFile) fieldNodes(name string) []string {
	if !ast.IsExported(name) {
		return []string{fieldNode(ff.cf.dir, name)}
	}
	f, dir := ff.g.fields, ff.cf.dir
	key := [2]string{dir, name}
	if nodes, ok := f.nodes[key]; ok {
		return nodes
	}
	reach, resolved := f.reachOf(dir), ff.e.modFor(dir) != nil
	nodes := []string{}
	for _, d := range f.declared[name] {
		if !resolved || reach[d] {
			nodes = append(nodes, fieldNode(d, name))
		}
	}
	f.nodes[key] = nodes
	return nodes
}

// writeField adds the edges of from written to a field of this name.
func (ff *flowFile) writeField(from, name string) {
	nodes := ff.fieldNodes(name)
	if len(nodes) == 0 {
		ff.g.edge(from, badFlow)
	}
	for _, node := range nodes {
		ff.g.edge(from, node)
	}
}

// writeSelector adds the edges of from assigned to sel.
func (ff *flowFile) writeSelector(from string, sel *ast.SelectorExpr) {
	if ff.outsideTheWalk(sel.X) {
		ff.g.edge(from, badFlow)
		return
	}
	ff.writeField(from, sel.Sel.Name)
}

// writeKeyed adds the edges of from given as the keyed name of a composite literal.
func (ff *flowFile) writeKeyed(from string, lit *ast.CompositeLit, name string) {
	if lit != nil && ff.typeOutsideTheWalk(lit.Type) {
		ff.g.edge(from, badFlow)
		return
	}
	ff.writeField(from, name)
}

// outsideTheWalk reports whether x, a selector's left side, is a package or a value
// whose spelled type comes from outside the tree.
func (ff *flowFile) outsideTheWalk(x ast.Expr) bool {
	for {
		switch t := x.(type) {
		case *ast.ParenExpr:
			x = t.X
			continue
		case *ast.StarExpr:
			x = t.X
			continue
		case *ast.IndexExpr:
			x = t.X
			continue
		}
		break
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	if id.Obj == nil {
		return true
	}
	switch d := id.Obj.Decl.(type) {
	case *ast.Field:
		return ff.typeOutsideTheWalk(d.Type)
	case *ast.ValueSpec:
		if d.Type != nil {
			return ff.typeOutsideTheWalk(d.Type)
		}
		for i, name := range d.Names {
			if name.Obj == id.Obj && i < len(d.Values) {
				return ff.valueOutsideTheWalk(d.Values[i])
			}
		}
	case *ast.AssignStmt:
		if len(d.Lhs) != len(d.Rhs) {
			return false
		}
		for i, lhs := range d.Lhs {
			if l, ok := lhs.(*ast.Ident); ok && l.Obj == id.Obj {
				return ff.valueOutsideTheWalk(d.Rhs[i])
			}
		}
	}
	return false
}

func (ff *flowFile) valueOutsideTheWalk(x ast.Expr) bool {
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		x = u.X
	}
	lit, ok := x.(*ast.CompositeLit)
	return ok && ff.typeOutsideTheWalk(lit.Type)
}

// typeOutsideTheWalk reports whether x spells pkg.T for an import the tree does not hold.
func (ff *flowFile) typeOutsideTheWalk(x ast.Expr) bool {
	switch t := x.(type) {
	case *ast.StarExpr:
		return ff.typeOutsideTheWalk(t.X)
	case *ast.ParenExpr:
		return ff.typeOutsideTheWalk(t.X)
	case *ast.IndexExpr:
		return ff.typeOutsideTheWalk(t.X)
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}
		ff.e.importTable(ff.cf, pkg.Name)
		_, inTree := ff.cf.importDirs[pkg.Name]
		return !inTree
	}
	return false
}
