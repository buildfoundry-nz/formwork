package sqlextract

import (
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// inert_flow_fields.go — where an exported field's value can go.
//
// A value of a struct exists only in the package that declares it and the packages
// that import that one, directly or not. So a field's node belongs to its declaring
// package, and a selector in package Q reaches the fields of its name declared in Q
// or anywhere Q imports. A selector whose receiver's declaration spells its struct
// reads or writes that struct's own field; any other reaches every field of its name
// in reach, which is the safe side of what it can mean.
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
	foreign  map[string]bool            // a file outside any module imports what this walk cannot place
	// typeFields is the fields each named struct declares itself, by owner.type.
	typeFields map[string]map[string]bool
	generic    map[string]bool // fields written through a selector spelling no type
	typed      map[string]bool // fields of a spelled type, by owner.type.name
	wire       map[string]bool // fields of a generated message, whose values leave on the wire
}

func newFlowFields() *flowFields {
	return &flowFields{declared: map[string][]string{}, own: map[string]map[string]bool{}, imports: map[string]map[string]bool{},
		reach: map[string]map[string]bool{}, nodes: map[[2]string][]string{}, foreign: map[string]bool{}, typeFields: map[string]map[string]bool{}, generic: map[string]bool{}, typed: map[string]bool{}, wire: map[string]bool{}}
}

func fieldNode(dir, name string) string { return "field " + dir + "." + name }

// A field has a written side and a read side, so a write and a read that both spell
// their struct meet only each other. A selector that spells no type stays conservative:
// its write reaches every read of the field, and its read sees every write.
func (f *flowFields) written(node string) string {
	f.generic[node] = true
	return node + "#written"
}

func (f *flowFields) typedWrite(typed, name string) string {
	f.typed[typed+"."+name] = true
	return "field " + typed + "." + name + "#written"
}

func (f *flowFields) typedRead(typed, name string) string {
	f.typed[typed+"."+name] = true
	return "field " + typed + "." + name
}

// link joins each field's sides. The generic node of a typed field is its owner's: a
// type is "dir.Type", and its field "dir.Type.Name".
func (f *flowFields) link(g *flowGraph) {
	for node := range f.generic {
		g.edge(node+"#written", node)
	}
	for typedField := range f.typed {
		i := strings.LastIndex(typedField, ".")
		j := strings.LastIndex(typedField[:i], ".")
		owner, name := typedField[:j], typedField[i+1:]
		read, write := "field "+typedField, "field "+typedField+"#written"
		generic := fieldNode(owner, name)
		g.edge(write, read)
		g.edge(write, generic)
		g.edge(generic+"#written", read)
		if f.wire[generic] {
			g.edge(write, badFlow)
		}
	}
	for node := range f.wire {
		g.edge(node+"#written", badFlow)
		g.edge(node, badFlow)
	}
}

// declareFields records every struct field the file spells and the directories it imports.
func (ff *flowFile) declareFields() {
	fields := ff.g.fields
	owner := ff.cf.dir
	fields.declareStructs(ff.file, owner)
	for _, is := range ff.file.Imports {
		ip, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		if dir := ff.e.importDir(ff.cf.dir, ip); dir != "" {
			fields.addImport(owner, dir)
		} else if ff.e.modFor(ff.cf.dir) == nil && strings.Contains(strings.SplitN(ip, "/", 2)[0], ".") {
			fields.foreign[owner] = true
		}
	}
}

func (f *flowFields) addImport(from, to string) {
	if f.imports[from] == nil {
		f.imports[from] = map[string]bool{}
	}
	f.imports[from][to] = true
}

func (f *flowFields) declareStructs(file *ast.File, dir string) {
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok {
			if st, ok := spec.Type.(*ast.StructType); ok && st.Fields != nil {
				own := map[string]bool{}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						own[name.Name] = true
					}
				}
				f.typeFields[dir+"."+spec.Name.Name] = own
			}
			return true
		}
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
func (g *flowGraph) declareGenerated(p string, file *ast.File) {
	dir := path.Dir(p)
	wire := newFlowFields()
	wire.declareStructs(file, dir)
	for name := range wire.own[dir] {
		g.fields.declare(dir, name)
		g.fields.wire[fieldNode(dir, name)] = true
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
// mean. An unexported field is its own package's, or in a directory of programs, is

func (ff *flowFile) fieldNodes(name string) []string {
	if !ast.IsExported(name) {
		return []string{fieldNode(ff.cf.dir, name)}
	}
	f, dir := ff.g.fields, ff.cf.dir
	key := [2]string{dir, name}
	if nodes, ok := f.nodes[key]; ok {
		return nodes
	}
	reach := f.reachOf(dir)
	open := false
	for d := range reach {
		open = open || (f.foreign[d] && ast.IsExported(name))
	}
	nodes := []string{}
	for _, d := range f.declared[name] {
		if open || reach[d] {
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
		ff.g.edge(from, ff.g.fields.written(node))
	}
}

// writeSelector adds the edges of from assigned to sel.
func (ff *flowFile) writeSelector(from string, sel *ast.SelectorExpr) {
	if ff.outsideTheWalk(sel.X) {
		ff.g.edge(from, badFlow)
		return
	}
	if typ := ff.spelledTypeOf(sel.X); typ != nil {
		if owner := ff.spelledField(typ, sel.Sel.Name); owner != "" {
			ff.g.edge(from, ff.g.fields.typedWrite(owner, sel.Sel.Name))
			return
		}
	}
	ff.writeField(from, sel.Sel.Name)
}

// writeKeyed adds the edges of from given as the keyed name of a composite literal.
func (ff *flowFile) writeKeyed(from string, lit *ast.CompositeLit, name string) {
	if lit != nil && ff.typeOutsideTheWalk(lit.Type) {
		ff.g.edge(from, badFlow)
		return
	}
	if lit != nil {
		if owner := ff.spelledField(lit.Type, name); owner != "" {
			ff.g.edge(from, ff.g.fields.typedWrite(owner, name))
			return
		}
	}
	ff.writeField(from, name)
}

// spelledField is the owner and name of the type x spells, as "dir.Type", when that type
// is a struct of this file's package, or a package this walk holds, declaring the field
// itself. A promoted field is not this type's, so it answers nothing.
func (ff *flowFile) spelledField(x ast.Expr, name string) string {
	switch t := x.(type) {
	case *ast.StarExpr:
		return ff.spelledField(t.X, name)
	case *ast.Ident:
		if typed := ff.cf.dir + "." + t.Name; ff.g.fields.typeFields[typed][name] {
			return typed
		}
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return ""
		}
		ff.e.importTable(ff.cf, pkg.Name)
		if dir, ok := ff.cf.importDirs[pkg.Name]; ok && ff.g.fields.typeFields[dir+"."+t.Sel.Name][name] {
			return dir + "." + t.Sel.Name
		}
	}
	return ""
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

// spelledTypeOf is the type a selector's left side was declared with, when an
// identifier's declaration spells one.
func (ff *flowFile) spelledTypeOf(x ast.Expr) ast.Expr {
	id, ok := x.(*ast.Ident)
	if !ok || id.Obj == nil {
		return nil
	}
	switch d := id.Obj.Decl.(type) {
	case *ast.Field:
		return d.Type
	case *ast.ValueSpec:
		return d.Type
	case *ast.AssignStmt:
		for i, lhs := range d.Lhs {
			l, ok := lhs.(*ast.Ident)
			if !ok || l.Obj != id.Obj {
				continue
			}
			if u, ok := d.Rhs[0].(*ast.UnaryExpr); ok && u.Op == token.RANGE {
				if i != 1 {
					return nil
				}
				return ff.elementType(u.X)
			}
			if len(d.Lhs) != len(d.Rhs) {
				return ff.resultType(d.Rhs[0], i)
			}
			value := d.Rhs[i]
			if u, ok := value.(*ast.UnaryExpr); ok && u.Op == token.AND {
				value = u.X
			}
			if lit, ok := value.(*ast.CompositeLit); ok {
				return lit.Type
			}
			return ff.resultType(value, 0)
		}
	}
	return nil
}

// elementType is the type of the values of x, a slice or a map whose type is spelled.
func (ff *flowFile) elementType(x ast.Expr) ast.Expr {
	var typ ast.Expr
	if lit, ok := x.(*ast.CompositeLit); ok {
		typ = lit.Type
	} else {
		typ = ff.spelledTypeOf(x)
	}
	switch t := typ.(type) {
	case *ast.ArrayType:
		return t.Elt
	case *ast.MapType:
		return t.Value
	}
	return nil
}

// resultType is the spelled type of result i of x, a call of a function of this
// package, whose unqualified type names mean what they mean here.
func (ff *flowFile) resultType(x ast.Expr, i int) ast.Expr {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if _, plain := call.Fun.(*ast.Ident); !plain {
		return nil
	}
	fn, known := ff.calledFunc(call)
	if !known || i >= len(ff.g.funcs[fn].results) {
		return nil
	}
	return ff.g.funcs[fn].results[i]
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

// importsName reports whether name is a package this file imports under that name. An
// unaliased import is named by its path's last element, or the one before a major
// version, which is as near as a file says: a package named otherwise reads as a value,
// the conservative side.
func (ff *flowFile) importsName(name string) bool {
	for _, is := range ff.file.Imports {
		if is.Name != nil {
			if is.Name.Name == name {
				return true
			}
			continue
		}
		ip, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		base := path.Base(ip)
		if strings.HasPrefix(base, "v") && len(base) > 1 && strings.Trim(base[1:], "0123456789") == "" {
			base = path.Base(path.Dir(ip))
		}
		if base == name {
			return true
		}
	}
	return false
}
