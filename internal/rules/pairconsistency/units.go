package pairconsistency

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// funcUnit is one same-func unit: a named, byte-bounded span of one file,
// with the 1-based line of its first byte for anchoring findings.
type funcUnit struct {
	name  string
	start int // byte offset of the unit's first byte
	end   int // byte offset one past the unit's last byte
	line  int // 1-based line of the unit's first byte
}

// sameFuncUnits extracts a file's function-grain units, dispatched on
// extension: .go via go/parser (exact spans), .dart and .proto via the
// brace-depth heuristics in dartunits.go and protounits.go (no Go parser
// exists for either). Any other extension has no unit vocabulary and yields
// no units — same-func says nothing about it, the documented contract a
// rule accepts by choosing same-func over same-file.
func sameFuncUnits(path string, content []byte) ([]funcUnit, error) {
	switch {
	case strings.HasSuffix(path, ".go"):
		return goFuncUnits(path, content)
	case strings.HasSuffix(path, ".dart"):
		return dartFuncUnits(path, content)
	case strings.HasSuffix(path, ".proto"):
		return protoFuncUnits(path, content)
	default:
		return nil, nil
	}
}

// goFuncUnits walks top-level FuncDecls (including methods) and var-bound
// func literals via go/parser. A parse failure is an engine error so a
// broken fixture cannot silently pass. Nested func-literals are not separate
// units — their text is part of the enclosing unit's span, which is what
// lets a projectreadscope.Memo(ctx, key, func() { …trigger… }) satisfy the
// unit.
func goFuncUnits(path string, content []byte) ([]funcUnit, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("pair-consistency same-func: parse %s: %w", path, err)
	}
	var units []funcUnit
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				// Bodyless declarations (assembly stubs, linkname externs)
				// have no span to owe a finding in.
				continue
			}
			units = append(units, goUnit(fset, funcDeclName(d), d))
		case *ast.GenDecl:
			// A package-level `var loadX = func(...) {...}` — the standard
			// test-seam refactor of `func loadX(...)` — is executable code no
			// FuncDecl owns. Each OUTERMOST func literal in a var initializer
			// (including an IIFE `= func(...){...}(nil)`) is its own unit,
			// named by its var, so the two-token stubbability refactor cannot
			// take a function out of the lockdown. Initializer code outside
			// any func literal deliberately stays un-united: definition sites
			// (`const theSQL = ...`) legitimately match the trigger, and
			// treating them as units would false-fire the rule on its own
			// vocabulary (disclosed in spec §5, pinned by the residue test).
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, value := range vs.Values {
					name := vs.Names[0].Name
					if len(vs.Names) == len(vs.Values) {
						name = vs.Names[i].Name
					}
					for _, fl := range outermostFuncLits(value) {
						units = append(units, goUnit(fset, name, fl))
					}
				}
			}
		}
	}
	return units, nil
}

func goUnit(fset *token.FileSet, name string, node ast.Node) funcUnit {
	return funcUnit{
		name:  name,
		start: fset.Position(node.Pos()).Offset,
		end:   fset.Position(node.End()).Offset,
		line:  fset.Position(node.Pos()).Line,
	}
}

// outermostFuncLits returns the func literals in e that are not nested inside
// another func literal. Descent stops at each one found: its nested literals
// belong to its span, mirroring how a FuncDecl owns its closures.
func outermostFuncLits(e ast.Expr) []*ast.FuncLit {
	var out []*ast.FuncLit
	ast.Inspect(e, func(n ast.Node) bool {
		if fl, ok := n.(*ast.FuncLit); ok {
			out = append(out, fl)
			return false
		}
		return true
	})
	return out
}

// funcDeclName is the short name used in findings: "Name" for a function,
// "(*T).Name" / "(T).Name" for a method — enough to tell free functions from
// ProjectScope methods in a pagerecompute-style lockdown.
func funcDeclName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := exprString(fn.Recv.List[0].Type)
	return "(" + recv + ")." + fn.Name.Name
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.IndexExpr:
		return exprString(t.X)
	case *ast.IndexListExpr:
		return exprString(t.X)
	default:
		return "T"
	}
}
