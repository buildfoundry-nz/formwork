package sqlextract

import (
	"go/ast"
	"go/token"
)

// inert.go — TEXT CONSUMED WHERE IT IS BUILT REACHES NO DRIVER.
//
// A detector matches a needle against a file, a test compares a line, an error names
// the statement it failed on. Those compositions read as SQL and none is a statement
// anything runs: the value ends right there, in a bool, an int, a compiled pattern or
// a message. So they are not compositions a SQL rule has to read.
//
// PROVED FROM THE TREE, NEVER DECLARED. The composition must stand directly in a known
// consumer, or in a local whose every use is one. There is no marker and no list of
// paths. Any other use, a return, an unknown call, a writer this cannot name, keeps
// it a site, so an analysis gap fails closed.

// consumingFuncs are the calls whose result cannot carry their text on, by package.
var consumingFuncs = map[string]map[string]bool{
	"strings": {"Contains": true, "ContainsAny": true, "HasPrefix": true, "HasSuffix": true, "Index": true,
		"LastIndex": true, "Count": true, "EqualFold": true},
	"bytes":  {"Contains": true, "HasPrefix": true, "HasSuffix": true, "Index": true, "Count": true},
	"regexp": {"MustCompile": true, "Compile": true, "MatchString": true, "MustCompilePOSIX": true},
	"errors": {"New": true},
	"fmt":    {"Errorf": true, "Print": true, "Printf": true, "Println": true},
	"log":    {"Print": true, "Printf": true, "Println": true, "Fatal": true, "Fatalf": true, "Panic": true, "Panicf": true},
	"slog":   {"Debug": true, "Info": true, "Warn": true, "Error": true},
}

// messageMethods report a message on whatever they are called on. None is a driver's.
var messageMethods = map[string]bool{
	"Errorf": true, "Error": true, "Fatalf": true, "Fatal": true, "Logf": true, "Log": true, "Skipf": true, "Skip": true,
}

// inertSpans is the spans of file whose text ends where it stands.
func inertSpans(file *ast.File) []span {
	var spans []span
	mark := func(x ast.Expr) { spans = append(spans, span{x.Pos(), x.End()}) }
	uses := map[*ast.Object]int{}
	consumed := map[*ast.Object]int{}
	values := map[*ast.Object]ast.Expr{}
	writes := map[*ast.Object]int{}
	consume := func(x ast.Expr) {
		if id, ok := x.(*ast.Ident); ok && id.Obj != nil {
			consumed[id.Obj]++
			return
		}
		mark(x)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op == token.EQL || v.Op == token.NEQ {
				consume(v.X)
				consume(v.Y)
			}
		case *ast.CallExpr:
			if !isConsumingCall(v) {
				return true
			}
			for _, a := range v.Args {
				consume(a)
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Obj == nil {
					continue
				}
				writes[id.Obj]++
				if v.Tok == token.DEFINE && len(v.Lhs) == len(v.Rhs) {
					values[id.Obj] = v.Rhs[i]
				}
			}
		case *ast.UnaryExpr:
			if v.Op == token.AND {
				if id := rootIdent(v.X); id != nil && id.Obj != nil {
					writes[id.Obj] += 2
				}
			}
		case *ast.Ident:
			if v.Obj != nil {
				uses[v.Obj]++
			}
		}
		return true
	})
	for obj, value := range values {
		// Its uses are the declaration plus the reads; each read must be consumed.
		if writes[obj] == 1 && consumed[obj] > 0 && uses[obj]-1 == consumed[obj] {
			mark(value)
		}
	}
	return spans
}

// isConsumingCall reports whether call's arguments end in it.
func isConsumingCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, isPkg := sel.X.(*ast.Ident)
	if isPkg && pkg.Obj == nil && consumingFuncs[pkg.Name][sel.Sel.Name] {
		return true
	}
	if isPkg && pkg.Obj == nil && pkg.Name == "fmt" && (sel.Sel.Name == "Fprint" || sel.Sel.Name == "Fprintf" || sel.Sel.Name == "Fprintln") {
		return len(call.Args) > 0 && isStdStream(call.Args[0])
	}
	if isPkg && pkg.Obj == nil {
		return false // a package's own function: nothing here says it consumes its text
	}
	return messageMethods[sel.Sel.Name]
}

// isStdStream reports whether x is os.Stdout or os.Stderr.
func isStdStream(x ast.Expr) bool {
	sel, ok := x.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && pkg.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}
