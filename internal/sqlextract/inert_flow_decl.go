package sqlextract

import (
	"go/ast"
	"go/token"
)

// inert_flow_decl.go — what inert_flow.go learns about a file before it walks one:
// its package-level functions, its unexported package variables, its builders and the
// files it opens, and the keys those take as nodes.

// declare records the file's package-level functions, its builders and its open files.
func (ff *flowFile) declare() {
	for _, d := range ff.file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		key := ff.funcKeyOf(fd)
		if _, again := ff.g.funcs[key]; again && fd.Recv != nil {
			// Methods of one name share a node, so their parameters must line up.
			if f := ff.g.funcs[key]; len(f.params) != paramCount(fd) {
				ff.g.edge(retKey(key), badFlow)
				for i := range f.params {
					ff.g.edge(paramKey(key, i), badFlow)
				}
			}
		}
		fn := &flowFunc{}
		if fd.Type.Results != nil {
			i := 0
			for _, r := range fd.Type.Results.List {
				n := max(len(r.Names), 1)
				for k := 0; k < n; k++ {
					if isIdent(r.Type, "string") || isByteSlice(r.Type) {
						fn.stringResults = append(fn.stringResults, i)
					}
					i++
				}
			}
		}
		for _, p := range fd.Type.Params.List {
			_, fn.variadic = p.Type.(*ast.Ellipsis)
			builder := isTextBuilderType(p.Type)
			if len(p.Names) == 0 {
				fn.params = append(fn.params, "")
				fn.builderParams = append(fn.builderParams, builder)
			}
			for _, name := range p.Names {
				if name.Name == "_" || name.Obj == nil {
					fn.params = append(fn.params, "")
				} else {
					fn.params = append(fn.params, ff.localKey(name.Obj))
					if builder {
						ff.builders[name.Obj] = true
					}
				}
				fn.builderParams = append(fn.builderParams, builder)
			}
		}
		if prior, again := ff.g.funcs[key]; !again || prior == nil {
			ff.g.funcs[key] = fn
		}
		for i, local := range fn.params {
			if local != "" {
				ff.g.edge(paramKey(key, i), local)
			}
		}
	}
	for _, d := range ff.file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, sp := range gen.Specs {
			if vs, ok := sp.(*ast.ValueSpec); ok {
				for _, name := range vs.Names {
					if !ast.IsExported(name.Name) && name.Name != "_" {
						ff.g.vars[ff.varKey(name.Name)] = true
					}
				}
			}
		}
	}
	ast.Inspect(ff.file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ValueSpec:
			if v.Type != nil && isTextBuilderType(v.Type) {
				for _, name := range v.Names {
					if name.Obj != nil {
						ff.builders[name.Obj] = true
					}
				}
			}
		case *ast.AssignStmt:
			if v.Tok != token.DEFINE || len(v.Rhs) != 1 {
				return true
			}
			id, ok := v.Lhs[0].(*ast.Ident)
			if !ok || id.Obj == nil {
				return true
			}
			if isOSFileCall(v.Rhs[0]) {
				ff.osFiles[id.Obj] = true
			}
			if isTextBuilderValue(v.Rhs[0]) {
				ff.builders[id.Obj] = true
			}
		}
		return true
	})
}

// funcKeyOf is fd's node: its directory and name, or for a method, its name alone. A
// call does not say which type's method it reaches, so every method of a name shares
// one node, and a value reaching any of them reaches all of them.
func (ff *flowFile) funcKeyOf(fd *ast.FuncDecl) string {
	if fd.Recv != nil {
		return "method." + fd.Name.Name
	}
	return ff.cf.dir + "." + fd.Name.Name
}

func paramCount(fd *ast.FuncDecl) int {
	n := 0
	for _, p := range fd.Type.Params.List {
		n += max(len(p.Names), 1)
	}
	return n
}

// varKey is an unexported package variable's node. Only its package can touch it, and
// every read is an identifier this walk sees.
func (ff *flowFile) varKey(name string) string { return "var " + ff.cf.dir + "." + name }

// fieldKey is a field's node. An unexported field can only be touched in its own
// package, so it shares a node with that package's fields of its name; an exported one
// shares a node across the module.
func (ff *flowFile) fieldKey(name string) string {
	if ast.IsExported(name) {
		return "field " + name
	}
	return "field " + ff.cf.dir + "." + name
}

func isByteSlice(x ast.Expr) bool {
	a, ok := x.(*ast.ArrayType)
	return ok && a.Len == nil && isIdent(a.Elt, "byte")
}

func isTextBuilderType(x ast.Expr) bool {
	if st, ok := x.(*ast.StarExpr); ok {
		x = st.X
	}
	sel, ok := x.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && ((pkg.Name == "strings" && sel.Sel.Name == "Builder") || (pkg.Name == "bytes" && sel.Sel.Name == "Buffer"))
}

func isTextBuilderValue(x ast.Expr) bool {
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		x = u.X
	}
	lit, ok := x.(*ast.CompositeLit)
	return ok && lit.Type != nil && isTextBuilderType(lit.Type)
}

func isOSFileCall(x ast.Expr) bool {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && pkg.Name == "os" && (sel.Sel.Name == "Create" || sel.Sel.Name == "OpenFile" || sel.Sel.Name == "CreateTemp")
}
