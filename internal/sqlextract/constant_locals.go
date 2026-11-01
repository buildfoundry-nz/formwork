package sqlextract

import (
	"go/ast"
	"go/token"
	"strings"
)

// constant_locals.go — A LOCAL WRITTEN ONCE HOLDS WHAT IT WAS WRITTEN.
//
// A query is often assembled from a name declared one line above it. A local
// declared with a value and never written again can only hold that value, so an
// operand naming it reads as that value when the value folds.
//
// EVERY OTHER WRITE DISQUALIFIES. A reassignment, a compound assignment, a range
// target, `&x`, and a write inside a closure are all writes this pass sees on the
// object itself. A value that depends on which write ran is not one value, and
// reading one of them would judge a query no path may produce.
//
// A SLICE IS READ ONLY WHERE strings.Join READS IT. Its elements can change
// through any function it is handed, so a slice local counts only when every use
// of it is the first argument of a strings.Join.
func constantLocals(file *ast.File) map[*ast.Object]ast.Expr {
	values := map[*ast.Object]ast.Expr{}
	barred := map[*ast.Object]bool{}
	uses := map[*ast.Object]int{}
	joins := map[*ast.Object]int{}
	bar := func(e ast.Expr) {
		if id := rootIdent(e); id != nil && id.Obj != nil {
			barred[id.Obj] = true
		}
	}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.AssignStmt:
				for i, lhs := range v.Lhs {
					id, plain := lhs.(*ast.Ident)
					if plain && id.Obj != nil && v.Tok == token.DEFINE && id.Obj.Decl == v && len(v.Lhs) == len(v.Rhs) {
						values[id.Obj] = v.Rhs[i]
						continue
					}
					bar(lhs)
				}
			case *ast.ValueSpec:
				for i, id := range v.Names {
					if id.Obj == nil || id.Obj.Kind != ast.Var {
						continue
					}
					if len(v.Names) == len(v.Values) {
						values[id.Obj] = v.Values[i]
					} else {
						barred[id.Obj] = true
					}
				}
			case *ast.RangeStmt:
				if v.Key != nil {
					bar(v.Key)
				}
				if v.Value != nil {
					bar(v.Value)
				}
			case *ast.IncDecStmt:
				bar(v.X)
			case *ast.UnaryExpr:
				if v.Op == token.AND {
					bar(v.X)
				}
			case *ast.CallExpr:
				if isStringsJoin(v) {
					if id, ok := v.Args[0].(*ast.Ident); ok && id.Obj != nil {
						joins[id.Obj]++
					}
				}
			case *ast.Ident:
				if v.Obj != nil {
					uses[v.Obj]++
				}
			}
			return true
		})
	}
	for obj, value := range values {
		if barred[obj] {
			delete(values, obj)
			continue
		}
		// The declaration itself is one use of the name.
		if _, slice := value.(*ast.CompositeLit); slice && uses[obj]-1 != joins[obj] {
			delete(values, obj)
		}
	}
	return values
}

// rootIdent is the variable a write lands on: x for x, x[i], x.f and (*x).
func rootIdent(e ast.Expr) *ast.Ident {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v
		case *ast.IndexExpr:
			e = v.X
		case *ast.SelectorExpr:
			e = v.X
		case *ast.StarExpr:
			e = v.X
		case *ast.ParenExpr:
			e = v.X
		case *ast.SliceExpr:
			e = v.X
		default:
			return nil
		}
	}
}

// isStringsJoin reports whether call is the strings package's two-argument Join,
// and not a local of that name.
func isStringsJoin(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Join" || len(call.Args) != 2 || call.Ellipsis != token.NoPos {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "strings" && pkg.Obj == nil
}

// foldJoin folds strings.Join over a []string literal, or a constant local
// holding one, when its separator and every element fold.
func (e *constEnv) foldJoin(call *ast.CallExpr, cf *constFile, env map[string]string, depth int) (string, bool) {
	sep, ok := e.fold(call.Args[1], cf, false, env, depth+1)
	if !ok {
		return "", false
	}
	list := call.Args[0]
	if id, ok := list.(*ast.Ident); ok && id.Obj != nil && cf != nil {
		list = cf.locals[id.Obj]
	}
	lit, ok := list.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	arr, ok := lit.Type.(*ast.ArrayType)
	if !ok || !isIdent(arr.Elt, "string") {
		return "", false
	}
	parts := make([]string, len(lit.Elts))
	for i, elt := range lit.Elts {
		if parts[i], ok = e.fold(elt, cf, false, env, depth+1); !ok {
			return "", false
		}
	}
	return strings.Join(parts, sep), true
}
