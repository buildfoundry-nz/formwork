package sqlextract

import (
	"go/ast"
	"go/token"
	"path"
	"strings"
)

// inert_flow_calls.go — how inert_flow.go follows text into a call: a known consumer
// ends it, a known transform carries it on, a module function takes it as a parameter,
// a builder holds it, and anything else is bad.

// passBuilder links a builder handed to a module function's builder parameter: what
// one writes the other holds. Anywhere else, the builder is out of sight.
func (ff *flowFile) passBuilder(from string, call *ast.CallExpr, arg ast.Expr, id *ast.Ident) {
	g := ff.g
	if isFprint(call) && len(call.Args) > 0 && call.Args[0] == arg {
		return
	}
	fn, known := ff.calledFunc(call)
	if !known || driverNames[strings.TrimPrefix(path.Ext(fn), ".")] {
		g.edge(from, badFlow)
		return
	}
	for j, a := range call.Args {
		if a != arg {
			continue
		}
		f := g.funcs[fn]
		if j < len(f.writerParams) && f.writerParams[j] {
			return // an io.Writer can only be written; passWriters follows what is
		}
		if j >= len(f.params) || f.params[j] == "" || !f.builderParams[j] {
			g.edge(from, badFlow)
			return
		}
		g.edge(from, f.params[j])
		g.edge(f.params[j], ff.localKey(id.Obj))
		return
	}
	g.edge(from, badFlow)
}

func isFprint(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Fprint")
}

// flowThroughCall handles cur as an argument of call. done means the flow ended here;
// otherwise next is the stack index the value continues from.
func (ff *flowFile) flowThroughCall(from string, call *ast.CallExpr, cur ast.Node) (next func(int) int, done bool) {
	g := ff.g
	up := func(i int) int { return i - 1 }
	switch f := call.Fun.(type) {
	case *ast.ArrayType:
		return up, false // []byte(x)
	case *ast.Ident:
		switch f.Name {
		case "append", "string":
			if f.Obj == nil {
				return up, false
			}
		case "panic", "len", "print", "println", "cap", "delete":
			if f.Obj == nil {
				return nil, true
			}
		}
	case *ast.SelectorExpr:
		if ff.isFileWrite(f) {
			return nil, true
		}
		if f.Sel.Name == "Replace" && isStringsReplacer(f.X) {
			return up, false
		}
		if id, ok := f.X.(*ast.Ident); ok && id.Obj != nil && ff.writerParams[id.Obj] != "" && (f.Sel.Name == "Write" || f.Sel.Name == "WriteString") {
			g.edge(from, ff.writerParams[id.Obj])
			return nil, true
		}
		if pkg, ok := f.X.(*ast.Ident); ok && pkg.Obj == nil {
			name := f.Sel.Name
			if transparentFuncs[pkg.Name][name] {
				return up, false
			}
			if isConsumingCall(call) || (pkg.Name == "bytes" && name == "Equal") || (pkg.Name == "reflect" && name == "DeepEqual") {
				return nil, true
			}
			if pkg.Name == "os" && name == "WriteFile" {
				return nil, true
			}
			if (pkg.Name == "fmt" && strings.HasPrefix(name, "Fprint")) || (pkg.Name == "io" && name == "WriteString") {
				ff.flowIntoWriter(from, call, cur)
				return nil, true
			}
		} else if id, ok := f.X.(*ast.Ident); ok && id.Obj != nil && ff.builders[id.Obj] {
			switch f.Sel.Name {
			case "WriteString", "Write", "WriteByte", "WriteRune":
				g.edge(from, ff.localKey(id.Obj))
				return nil, true
			}
		}
		if messageMethods[f.Sel.Name] && isConsumingCall(call) {
			return nil, true
		}
		if pkg, ok := f.X.(*ast.Ident); ok && pkg.Obj == nil {
			if pathFuncs[pkg.Name][f.Sel.Name] {
				return nil, true
			}
		}
		if regexpConsuming[f.Sel.Name] {
			return nil, true
		}
		if regexpTransparent[f.Sel.Name] {
			return up, false
		}
	}
	fn, known := ff.calledFunc(call)
	if !known || driverNames[strings.TrimPrefix(path.Ext(fn), ".")] {
		g.edge(from, badFlow)
		return nil, true
	}
	info := g.funcs[fn]
	for j, a := range call.Args {
		if a != cur {
			continue
		}
		if j >= len(info.params) {
			if !info.variadic || len(info.params) == 0 {
				g.edge(from, badFlow)
				return nil, true
			}
			j = len(info.params) - 1
		}
		if info.params[j] == "" {
			g.edge(from, badFlow)
			return nil, true
		}
		g.edge(from, paramKey(fn, j))
		return nil, true
	}
	g.edge(from, badFlow)
	return nil, true
}

// isFileWrite reports whether f is a write method of a standard stream or an opened file.
func (ff *flowFile) isFileWrite(f *ast.SelectorExpr) bool {
	if f.Sel.Name != "WriteString" && f.Sel.Name != "Write" {
		return false
	}
	if isStdStream(f.X) {
		return true
	}
	id, ok := f.X.(*ast.Ident)
	return ok && id.Obj != nil && ff.osFiles[id.Obj]
}

// flowIntoWriter ends text at a stream or an opened file, or carries it into a builder.
// Any other writer is out of sight.
func (ff *flowFile) flowIntoWriter(from string, call *ast.CallExpr, cur ast.Node) {
	if len(call.Args) == 0 || call.Args[0] == cur {
		return
	}
	w := call.Args[0]
	if isStdStream(w) {
		return
	}
	if u, ok := w.(*ast.UnaryExpr); ok && u.Op == token.AND {
		w = u.X
	}
	if id, ok := w.(*ast.Ident); ok && id.Obj != nil {
		if ff.osFiles[id.Obj] {
			return
		}
		if ff.builders[id.Obj] {
			ff.g.edge(from, ff.localKey(id.Obj))
			return
		}
		if node := ff.writerParams[id.Obj]; node != "" {
			ff.g.edge(from, node)
			return
		}
	}
	ff.g.edge(from, badFlow)
}

// isStringsReplacer reports whether x is a strings.NewReplacer call, or a variable
// declared as one. Its Replace returns its text argument onward, and that is all the
// flow asks of it: text given to NewReplacer itself is still an unknown call's.
func isStringsReplacer(x ast.Expr) bool {
	if id, ok := x.(*ast.Ident); ok && id.Obj != nil {
		switch d := id.Obj.Decl.(type) {
		case *ast.AssignStmt:
			if len(d.Lhs) != len(d.Rhs) {
				return false
			}
			for i, lhs := range d.Lhs {
				if l, ok := lhs.(*ast.Ident); ok && l.Obj == id.Obj {
					x = d.Rhs[i]
				}
			}
		case *ast.ValueSpec:
			for i, name := range d.Names {
				if name.Obj == id.Obj && i < len(d.Values) {
					x = d.Values[i]
				}
			}
		}
	}
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "NewReplacer" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && pkg.Name == "strings"
}
