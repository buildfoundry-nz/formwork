package gotypes

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// go/forbidden-type-in-position reports a listed type wherever it is the type
// of a listed position, resolved with go/types: written directly, through an
// alias declared in any package, or inferred.

type forbiddenTypeParams struct {
	Types     []string `yaml:"types" json:"types"`
	Positions []string `yaml:"positions" json:"positions"`
}

// positionNames are the positions a rule may list. "elem" and "map" are nested
// positions: they are found inside the type of any other position, at any depth.
var positionNames = map[string]bool{
	"field": true, "param": true, "result": true, "var": true,
	"elem": true, "map": true, "type-arg": true,
}

type forbiddenType struct {
	types     []string
	positions map[string]bool
}

func newForbiddenType(raw json.RawMessage) (check, error) {
	var p forbiddenTypeParams
	if err := strictJSON(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Types) == 0 {
		return nil, errors.New("params.types is required")
	}
	if len(p.Positions) == 0 {
		return nil, errors.New("params.positions is required")
	}
	c := &forbiddenType{types: p.Types, positions: map[string]bool{}}
	for _, name := range p.Positions {
		if !positionNames[name] {
			return nil, fmt.Errorf("params.positions: unknown position %q", name)
		}
		c.positions[name] = true
	}
	return c, nil
}

// match reports the listed name t is, if any. "any" is the empty interface by
// any spelling, a defined type whose underlying type it is included; a type
// parameter is never it, since its underlying type is its constraint. Any other
// name is compared with the type's qualified spelling, aliases resolved.
func (c *forbiddenType) match(t types.Type) (string, bool) {
	if t == nil {
		return "", false
	}
	t = types.Unalias(t)
	for _, name := range c.types {
		if name == "any" {
			if _, isParam := t.(*types.TypeParam); isParam {
				continue
			}
			if iface, ok := t.Underlying().(*types.Interface); ok && iface.Empty() {
				return name, true
			}
			continue
		}
		if types.TypeString(t, nil) == name {
			return name, true
		}
	}
	return "", false
}

// report checks t at one position, then the element, key and value types
// inside it. A function or struct type inside t is not descended: where it is
// written, the syntax walk reaches its params and fields itself.
func (c *forbiddenType) report(pass *analysis.Pass, pos token.Pos, position, what string, t types.Type) {
	if c.positions[position] {
		if name, ok := c.match(t); ok {
			pass.Report(analysis.Diagnostic{Pos: pos, Message: fmt.Sprintf("%s is %s, a type this rule forbids", what, name)})
			return
		}
	}
	if t == nil {
		return
	}
	switch u := types.Unalias(t).(type) {
	case *types.Slice:
		c.report(pass, pos, "elem", "elem", u.Elem())
	case *types.Array:
		c.report(pass, pos, "elem", "elem", u.Elem())
	case *types.Pointer:
		c.report(pass, pos, "elem", "elem", u.Elem())
	case *types.Chan:
		c.report(pass, pos, "elem", "elem", u.Elem())
	case *types.Map:
		c.report(pass, pos, "map", "map key", u.Key())
		c.report(pass, pos, "map", "map value", u.Elem())
	}
}

func (c *forbiddenType) run(pass *analysis.Pass) {
	info := pass.TypesInfo
	fields := func(list *ast.FieldList, position string) {
		if list == nil {
			return
		}
		for _, f := range list.List {
			expr := f.Type
			if ell, ok := expr.(*ast.Ellipsis); ok {
				expr = ell.Elt
			}
			c.report(pass, expr.Pos(), position, position, info.TypeOf(expr))
		}
	}
	defined := func(id *ast.Ident) {
		if id == nil || id.Name == "_" {
			return
		}
		if obj, ok := info.Defs[id].(*types.Var); ok {
			c.report(pass, id.Pos(), "var", "var", obj.Type())
		}
	}
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.StructType:
				fields(n.Fields, "field")
			case *ast.FuncType:
				fields(n.Params, "param")
				fields(n.Results, "result")
			case *ast.ValueSpec:
				if n.Type != nil {
					c.report(pass, n.Type.Pos(), "var", "var", info.TypeOf(n.Type))
					break
				}
				for _, id := range n.Names {
					defined(id)
				}
			case *ast.AssignStmt:
				if n.Tok == token.DEFINE {
					for _, lhs := range n.Lhs {
						id, _ := lhs.(*ast.Ident)
						defined(id)
					}
				}
			case *ast.RangeStmt:
				if n.Tok == token.DEFINE {
					id, _ := n.Key.(*ast.Ident)
					defined(id)
					id, _ = n.Value.(*ast.Ident)
					defined(id)
				}
			}
			return true
		})
		for id, inst := range info.Instances {
			if id.Pos() < file.Pos() || id.Pos() > file.End() {
				continue
			}
			for i := 0; i < inst.TypeArgs.Len(); i++ {
				c.report(pass, id.Pos(), "type-arg", "type-arg", inst.TypeArgs.At(i))
			}
		}
	}
}
