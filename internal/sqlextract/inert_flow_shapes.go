package sqlextract

import (
	"go/ast"
	"go/token"
	"strconv"
)

// inert_flow_shapes.go — three shapes a value takes that the walk can follow exactly.
//
// A LOCAL CLOSURE ONLY EVER CALLED is a function with one name in one scope, so its
// parameters and its returns are nodes like any function's, including the recursive
// form declared as a variable and assigned once after. A closure assigned twice,
// passed on or returned can run anywhere, and its returns stay bad.
//
// A POSITIONAL ROW of a struct this file spells is the keyed row it means: the table
// of a table test writes each column to that column's field.
//
// A WRITER PARAMETER holds what its callers hand it. Text written to one ends where
// every caller's argument ends: a stream, an opened file or a builder. Any other
// argument, or a function nothing here calls, is out of sight.

func closureKey(path string, offset int) string {
	return "closure " + path + ":" + strconv.Itoa(offset)
}

func writerKey(fn string, i int) string { return "writer " + fn + " " + strconv.Itoa(i) }

// declareClosures finds the file's closures that are only ever called.
func (ff *flowFile) declareClosures() {
	uses, calls := map[*ast.Object]int{}, map[*ast.Object]int{}
	bound := map[*ast.Object]*ast.FuncLit{}
	late, twice := map[*ast.Object]bool{}, map[*ast.Object]bool{} // var f func(...); f = func...
	bind := func(name *ast.Ident, value ast.Expr) {
		if lit, ok := value.(*ast.FuncLit); ok && name.Obj != nil {
			bound[name.Obj] = lit
		}
	}
	ast.Inspect(ff.file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if v.Obj != nil {
				uses[v.Obj]++
			}
		case *ast.CallExpr:
			switch f := v.Fun.(type) {
			case *ast.Ident:
				if f.Obj != nil {
					calls[f.Obj]++
				}
			case *ast.FuncLit:
				ff.declareClosure(f)
			}
		case *ast.AssignStmt:
			if len(v.Lhs) != len(v.Rhs) {
				return true
			}
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if v.Tok == token.DEFINE {
					bind(id, v.Rhs[i])
				} else if spec, ok := declaredFuncVar(id); ok && v.Tok == token.ASSIGN {
					if _, again := bound[id.Obj]; again {
						spec = nil // assigned twice: it can be either
						delete(bound, id.Obj)
						twice[id.Obj] = true
					}
					if spec != nil && !twice[id.Obj] {
						bind(id, v.Rhs[i])
						late[id.Obj] = true
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range v.Names {
				if i < len(v.Values) && !(ff.file.Scope != nil && ff.file.Scope.Objects[name.Name] == name.Obj) {
					bind(name, v.Values[i])
				}
			}
		}
		return true
	})
	for obj, lit := range bound {
		// The binding's own identifier is the one use that is not a call, and a
		// variable declared first and assigned after has two.
		bindings := 1
		if late[obj] {
			bindings = 2
		}
		if calls[obj] > 0 && uses[obj] == calls[obj]+bindings {
			ff.closures[obj] = ff.declareClosure(lit)
		}
	}
}

// declaredFuncVar reports whether id names a variable declared with a function type
// and no value, the shape a recursive closure needs.
func declaredFuncVar(id *ast.Ident) (*ast.ValueSpec, bool) {
	if id.Obj == nil {
		return nil, false
	}
	spec, ok := id.Obj.Decl.(*ast.ValueSpec)
	if !ok || len(spec.Values) != 0 {
		return nil, false
	}
	_, isFunc := spec.Type.(*ast.FuncType)
	return spec, isFunc
}

func (ff *flowFile) declareClosure(lit *ast.FuncLit) string {
	key := closureKey(ff.path, ff.fset.Position(lit.Pos()).Offset)
	ff.closureLits[lit] = key
	ff.declareFunc(key, lit.Type)
	return key
}

// structRow reports the field that position of cur in the positional row lit names.
func (ff *flowFile) structRow(lit *ast.CompositeLit, cur ast.Node, typ ast.Expr) (string, bool) {
	st := ff.structOf(typ)
	if st == nil || st.Fields == nil {
		return "", false
	}
	at := -1
	for k, elt := range lit.Elts {
		if elt == cur {
			at = k
		}
	}
	for _, field := range st.Fields.List {
		names := []string{}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		if len(field.Names) == 0 {
			names = append(names, embeddedName(field.Type))
		}
		for _, name := range names {
			if at == 0 {
				return name, name != "" && name != "_"
			}
			at--
		}
	}
	return "", false
}

// structOf is the struct type x spells or names in this file, or nil.
func (ff *flowFile) structOf(x ast.Expr) *ast.StructType {
	switch t := x.(type) {
	case *ast.StarExpr:
		return ff.structOf(t.X)
	case *ast.StructType:
		return t
	case *ast.Ident:
		if t.Obj != nil {
			if spec, ok := t.Obj.Decl.(*ast.TypeSpec); ok {
				return ff.structOf(spec.Type)
			}
		}
	}
	return nil
}

// litType is the type of the composite literal at stack index i: its own, or for an
// elided one what the literal around it says its elements are.
func (ff *flowFile) litType(i int) ast.Expr {
	lit, ok := ff.stack[i].(*ast.CompositeLit)
	if !ok {
		return nil
	}
	if lit.Type != nil {
		return lit.Type
	}
	if i < 1 {
		return nil
	}
	up := i - 1
	if kv, ok := ff.stack[up].(*ast.KeyValueExpr); ok {
		if kv.Value != ast.Expr(lit) || up < 1 {
			return nil
		}
		up--
	}
	switch t := ff.litType(up).(type) {
	case *ast.ArrayType:
		return t.Elt
	case *ast.MapType:
		return t.Value
	}
	return nil
}

func isWriterType(x ast.Expr) bool {
	if st, ok := x.(*ast.StarExpr); ok {
		sel, ok := st.X.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Obj == nil && pkg.Name == "os" && sel.Sel.Name == "File"
	}
	sel, ok := x.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && pkg.Name == "io" && sel.Sel.Name == "Writer"
}

// passWriters adds what call hands fn's writer parameters.
func (ff *flowFile) passWriters(call *ast.CallExpr, fn string) {
	g := ff.g
	writers := g.funcs[fn].writerParams
	if len(writers) == 0 {
		return
	}
	for j, arg := range call.Args {
		if j >= len(writers) || !writers[j] {
			continue
		}
		node := writerKey(fn, j)
		if isStdStream(arg) {
			continue
		}
		w := arg
		if u, ok := w.(*ast.UnaryExpr); ok && u.Op == token.AND {
			w = u.X
		}
		id, ok := w.(*ast.Ident)
		switch {
		case ok && id.Obj != nil && ff.osFiles[id.Obj]:
		case ok && id.Obj != nil && ff.builders[id.Obj]:
			g.edge(node, ff.localKey(id.Obj))
		case ok && id.Obj != nil && ff.writerParams[id.Obj] != "":
			g.edge(node, ff.writerParams[id.Obj])
		default:
			g.edge(node, badFlow)
		}
	}
}
