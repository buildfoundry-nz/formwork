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
		if fd.Recv == nil {
			ff.noteFuncRange(fd)
		}
		if fd.Recv != nil {
			ff.g.noteMethod(fd.Name.Name, key)
		}
		if _, again := ff.g.funcs[key]; again && fd.Recv != nil {
			// A type's method declared twice, in files a build tag tells apart, is one
			// node, so their parameters must line up.
			if f := ff.g.funcs[key]; len(f.params) != paramCount(fd) {
				ff.g.edge(retKey(key), badFlow)
				for i := range f.params {
					ff.g.edge(paramKey(key, i), badFlow)
				}
			}
		}
		fn := &flowFunc{stringResults: stringResultsOf(fd.Type)}
		ff.fillFunc(fn, key, fd.Type)
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
	ff.declareClosures()
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

// fillFunc records fn's parameters under key and links each to its local.
func (ff *flowFile) fillFunc(fn *flowFunc, key string, ft *ast.FuncType) {
	if ft.Results != nil {
		for _, r := range ft.Results.List {
			for k := 0; k < max(len(r.Names), 1); k++ {
				fn.results = append(fn.results, r.Type)
			}
		}
	}
	for _, p := range ft.Params.List {
		_, fn.variadic = p.Type.(*ast.Ellipsis)
		builder, writer, plain := isTextBuilderType(p.Type), isWriterType(p.Type), isPlainType(p.Type)
		if len(p.Names) == 0 {
			fn.params = append(fn.params, "")
			fn.builderParams = append(fn.builderParams, builder)
			fn.writerParams = append(fn.writerParams, writer)
			fn.plainParams = append(fn.plainParams, plain)
		}
		for _, name := range p.Names {
			i := len(fn.params)
			if name.Name == "_" || name.Obj == nil {
				fn.params = append(fn.params, "")
			} else {
				fn.params = append(fn.params, ff.localKey(name.Obj))
				if builder {
					ff.builders[name.Obj] = true
				}
				if writer {
					ff.writerParams[name.Obj] = writerKey(key, i)
					ff.g.writers[writerKey(key, i)] = key
				}
			}
			fn.builderParams = append(fn.builderParams, builder)
			fn.writerParams = append(fn.writerParams, writer)
			fn.plainParams = append(fn.plainParams, plain)
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

// declareFunc records a closure's results and parameters under key.
func (ff *flowFile) declareFunc(key string, ft *ast.FuncType) {
	if _, again := ff.g.funcs[key]; again {
		return
	}
	fn := &flowFunc{stringResults: stringResultsOf(ft)}
	ff.fillFunc(fn, key, ft)
}

func stringResultsOf(ft *ast.FuncType) []int {
	if ft.Results == nil {
		return nil
	}
	var results []int
	i := 0
	for _, r := range ft.Results.List {
		n := max(len(r.Names), 1)
		for k := 0; k < n; k++ {
			if isIdent(r.Type, "string") || isByteSlice(r.Type) {
				results = append(results, i)
			}
			i++
		}
	}
	return results
}

// funcKeyOf is fd's node: its directory and name, or for a method, its directory, its
// receiver's type and its name. A method of one name on another type is another node.
func (ff *flowFile) funcKeyOf(fd *ast.FuncDecl) string {
	if fd.Recv != nil {
		return methodKey(ff.cf.dir+"."+receiverTypeName(fd.Recv), fd.Name.Name)
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

// isPlainType reports whether x is a predeclared type that cannot hold text. A byte and
// a rune are left out, because a loop over text passes it on a byte at a time.
func isPlainType(x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Name {
	case "bool", "int", "int8", "int16", "int32", "int64", "uint", "uint16", "uint32", "uint64", "uintptr", "float32", "float64":
		return id.Obj == nil
	}
	return false
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
