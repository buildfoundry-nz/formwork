package sqlextract

import (
	"go/ast"
	"go/token"
	"strconv"
)

// parsed_uuid.go — A PARSED UUID CANNOT CARRY STATEMENT TEXT.
//
// A plan can depend on a tenant id being a literal, so the query spells one where a
// bind would change the plan, and uuid.Parse is the check that makes that safe. The
// String of what it returned is hex and dashes, so it stands in a statement the way
// an integer does (integerHole): the statement parses as it will at run time, and no
// predicate can have a table hidden in it.
//
// ONE SHAPE ONLY. A local whose one write is `p, err := uuid.Parse(s)` or
// `p := uuid.MustParse(s)`, from github.com/google/uuid, read as p.String() in the
// file that parses it. A parameter, a field, a reassigned or addressed local, or the
// string that was parsed all stay unread: nothing here can prove what they hold.

// parsedUUIDText is the text read for a parsed uuid's String.
const parsedUUIDText = "00000000-0000-0000-0000-000000000000"

// parsedUUIDs is the locals of file that can only hold a parsed uuid.
func parsedUUIDs(file *ast.File) map[*ast.Object]bool {
	name := uuidImportName(file)
	if name == "" {
		return nil
	}
	parsed := map[*ast.Object]bool{}
	writes := map[*ast.Object]int{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range v.Lhs {
				if id := rootIdent(lhs); id != nil && id.Obj != nil {
					writes[id.Obj]++
				}
			}
			if v.Tok == token.DEFINE && len(v.Rhs) == 1 && isUUIDParse(v.Rhs[0], name) {
				if id, ok := v.Lhs[0].(*ast.Ident); ok && id.Obj != nil {
					parsed[id.Obj] = true
				}
			}
		case *ast.RangeStmt:
			for _, x := range []ast.Expr{v.Key, v.Value} {
				if id := rootIdent(x); x != nil && id != nil && id.Obj != nil {
					writes[id.Obj] += 2
				}
			}
		case *ast.UnaryExpr:
			if v.Op == token.AND {
				if id := rootIdent(v.X); id != nil && id.Obj != nil {
					writes[id.Obj] += 2
				}
			}
		}
		return true
	})
	for obj := range parsed {
		if writes[obj] != 1 {
			delete(parsed, obj)
		}
	}
	return parsed
}

// uuidImportName is the name file imports github.com/google/uuid under.
func uuidImportName(file *ast.File) string {
	for _, is := range file.Imports {
		path, err := strconv.Unquote(is.Path.Value)
		if err != nil || path != "github.com/google/uuid" {
			continue
		}
		if is.Name != nil {
			return is.Name.Name
		}
		return "uuid"
	}
	return ""
}

func isUUIDParse(x ast.Expr, name string) bool {
	call, ok := x.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == name && pkg.Obj == nil && (sel.Sel.Name == "Parse" || sel.Sel.Name == "MustParse")
}

// isParsedUUIDString reports whether call is String on a parsed uuid of cf's file.
func isParsedUUIDString(call *ast.CallExpr, cf *constFile) bool {
	if cf == nil || len(cf.parsedUUIDs) == 0 || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "String" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Obj != nil && cf.parsedUUIDs[id.Obj]
}
