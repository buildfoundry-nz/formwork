package sqlextract

import (
	"go/ast"
	"go/token"
)

// package_vars.go — A PACKAGE VARIABLE NOTHING WRITES HOLDS ITS INITIALISER.
//
// A statement is often assembled from a `var` because one of its parts is a SQL
// template call, which Go will not let a `const` hold. Such a variable reads as
// its initialiser when all of these hold:
//
//   - it is unexported, so only its own package can write it;
//   - it has one value of its own, and is untyped or typed string;
//   - no file of the package assigns it, appends to it, ranges into it, or takes
//     its address. A write to a local of the same name counts too: names are all
//     this table has, and refusing a read is the safe direction.
//
// A package read without its tests is read without their writes, because those
// writes are not in the program being judged.

// addPackageVar records n as a candidate when spec i of sp qualifies on its own.
func addPackageVar(t *pkgTable, sp *ast.ValueSpec, i int, cf *constFile) {
	n := sp.Names[i]
	if ast.IsExported(n.Name) || len(sp.Names) != len(sp.Values) {
		return
	}
	if sp.Type != nil && !isIdent(sp.Type, "string") {
		return
	}
	t.vars[n.Name] = constDef{expr: sp.Values[i], file: cf}
}

// addPackageWrites records every name file writes.
func addPackageWrites(t *pkgTable, file *ast.File) {
	bar := func(x ast.Expr) {
		if id := rootIdent(x); id != nil {
			t.written[id.Name] = true
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range v.Lhs {
				bar(lhs)
			}
		case *ast.RangeStmt:
			if v.Tok != token.ASSIGN {
				return true
			}
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
		}
		return true
	})
}

// fileVars is the variables file declares at package level.
func fileVars(file *ast.File) map[*ast.Object]bool {
	vars := map[*ast.Object]bool{}
	if file.Scope == nil {
		return vars
	}
	for _, obj := range file.Scope.Objects {
		if obj.Kind == ast.Var {
			vars[obj] = true
		}
	}
	return vars
}
