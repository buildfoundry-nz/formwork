package sqlextract

import (
	"go/ast"
	"go/token"
)

// templates.go — A SQL TEMPLATE IS READ WHERE IT IS CALLED.
//
// A repository keeps a predicate in one place by making it a function of the
// aliases and placeholders it is spliced in with: VisibleWhere("p"),
// OverdueSQL("$1"), LeftJoinOrderCustomer("so", "so.org_id"). Its body is a
// composition of literals and its parameters, so the extractor could not read
// it at the definition, and a query calling it was a composition with a call
// for an operand, so it could not read that either. Rules guard these homes —
// a credential on the function makes its declaration the one place the
// predicate may be written — so spelling each instantiation out as a constant
// trades the gate's coverage for its single-source guarantee.
//
// A TEMPLATE is a package-level function whose parameters and one result are
// all `string` and whose body is a single `return` of a string expression.
// Called with arguments that are constants, its result is a constant too: the
// extractor binds the parameters and folds the body (fold, callTemplate), and
// the call reads as that literal wherever it stands — an operand, an argument,
// an assignment, a return. Templates may call templates.
//
// AT THE DEFINITION, ONE CANDIDATE PER INSTANCE. A rule scoped to the package
// that writes a predicate expects to judge it wherever it is spliced, so the
// body is judged where it is written, once per distinct set of arguments the
// walk calls it with (template_index.go), and those compositions are then not
// sites. A template some call passes a non-constant, or that nothing in the
// walk calls, cannot be judged whole: nothing says what it builds, so its
// compositions stay sites, as any composition's are.
//
// AT THE CALL, A LITERAL. A call whose arguments are constants reads as the
// instance it builds, so a rule scoped to the caller judges it too. A call
// passing a non-constant is a Site ("a SQL template called with an argument
// that is not a constant") when it stands on its own, and part of its
// enclosing composition's Site when it is a '+' operand. Inside another
// template's body a call takes that template's parameters and is instantiated
// from that template's instances in turn.
//
// RESIDUAL. A method, a function with any other parameter type, or a body of
// more than a return is not a template, and a call to it is the operand it
// always was. Without a walk (scan.NewMemFile) no call is indexed, so a
// template's body is a site as before; calls within the file still read.

func isTemplate(fd *ast.FuncDecl) bool {
	if fd == nil || fd.Recv != nil || fd.Type.TypeParams != nil || fd.Body == nil || len(fd.Body.List) != 1 {
		return false
	}
	res := fd.Type.Results
	if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 || !isIdent(res.List[0].Type, "string") {
		return false
	}
	for _, f := range fd.Type.Params.List {
		if !isIdent(f.Type, "string") {
			return false
		}
	}
	rs, ok := fd.Body.List[0].(*ast.ReturnStmt)
	return ok && len(rs.Results) == 1
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name && id.Obj == nil
}

// templateFunc resolves fun to a SQL template and the file declaring it.
func (e *constEnv) templateFunc(fun ast.Expr, cf *constFile) (*ast.FuncDecl, *constFile) {
	switch f := fun.(type) {
	case *ast.Ident:
		if f.Obj != nil {
			fd, ok := f.Obj.Decl.(*ast.FuncDecl)
			if ok && f.Obj.Kind == ast.Fun && isTemplate(fd) {
				return fd, cf
			}
			return nil, nil
		}
		if e == nil {
			return nil, nil
		}
		t := e.ownTable(cf)
		if def, ok := t.templates[f.Name]; ok && t.decls[f.Name] == 1 {
			return def.decl, def.file
		}
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		if !ok || x.Obj != nil || e == nil || !ast.IsExported(f.Sel.Name) {
			return nil, nil
		}
		t := e.importTable(cf, x.Name)
		if t == nil {
			return nil, nil
		}
		if def, ok := t.templates[f.Sel.Name]; ok && t.decls[f.Sel.Name] == 1 {
			return def.decl, def.file
		}
	}
	return nil, nil
}

// bindings pairs fd's parameter names with args, folding each in the caller's
// file and parameters. known is false for an argument that does not fold;
// its value is then absent.
func (e *constEnv) bindings(fd *ast.FuncDecl, args []ast.Expr, cf *constFile, env map[string]string, depth int) (bound map[string]string, all bool, ok bool) {
	var names []string
	for _, f := range fd.Type.Params.List {
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	if len(names) != len(args) {
		return nil, false, false
	}
	bound = map[string]string{}
	all = true
	for i, a := range args {
		val, known := e.fold(a, cf, false, env, depth+1)
		if !known {
			all = false
			continue
		}
		bound[names[i]] = val
	}
	return bound, all, true
}

// callTemplate evaluates call when it calls a SQL template with arguments
// that are all constants.
func (e *constEnv) callTemplate(call *ast.CallExpr, cf *constFile, env map[string]string, depth int) (string, bool) {
	fd, fcf := e.templateFunc(call.Fun, cf)
	if fd == nil {
		return "", false
	}
	bound, all, ok := e.bindings(fd, call.Args, cf, env, depth)
	if !ok || !all {
		return "", false
	}
	return e.fold(fd.Body.List[0].(*ast.ReturnStmt).Results[0], fcf, false, bound, depth+1)
}

// render is fold that does not fail: what it cannot read becomes the
// placeholder, so an unread template call still has text the census and the
// SQL test can judge.
func (e *constEnv) render(expr ast.Expr, cf *constFile, env map[string]string, depth int) string {
	if s, ok := e.fold(expr, cf, false, env, depth); ok {
		return s
	}
	if depth > 64 {
		return Placeholder
	}
	switch v := expr.(type) {
	case *ast.ParenExpr:
		return e.render(v.X, cf, env, depth+1)
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return e.render(v.X, cf, env, depth+1) + e.render(v.Y, cf, env, depth+1)
		}
	case *ast.CallExpr:
		if fd, fcf := e.templateFunc(v.Fun, cf); fd != nil {
			if bound, _, ok := e.bindings(fd, v.Args, cf, env, depth); ok {
				return e.render(fd.Body.List[0].(*ast.ReturnStmt).Results[0], fcf, bound, depth+1)
			}
		}
	}
	return Placeholder
}

// anywhere reads x when it calls a SQL template: as a literal when the
// arguments are constants, and otherwise — outside a template's own body,
// where the parameters are not yet bound — as a call the extractor could not
// read, recorded when report is true.
func anywhere(e *constEnv, cf *constFile, fset *token.FileSet, x ast.Expr, literal func(ast.Expr, string) ast.Expr, out *folded, report bool) ast.Expr {
	call, ok := x.(*ast.CallExpr)
	if !ok || call.Ellipsis != token.NoPos {
		return x
	}
	if s, ok := e.callTemplate(call, cf, nil, 0); ok {
		return literal(x, s)
	}
	if !report || out.inTemplate(x) || out.inUnread(x) {
		return x
	}
	fd, fcf := e.templateFunc(call.Fun, cf)
	if fd == nil {
		return x
	}
	text := Placeholder
	if bound, _, ok := e.bindings(fd, call.Args, cf, nil, 0); ok {
		text = e.render(fd.Body.List[0].(*ast.ReturnStmt).Results[0], fcf, bound, 1)
	}
	out.unreadSpans = append(out.unreadSpans, span{call.Pos(), call.End()})
	out.unread = append(out.unread, Site{
		Path:   cf.path,
		Line:   fset.Position(call.Pos()).Line,
		Key:    keyTemplateCall,
		Reason: reasonTemplateCall,
		Text:   text,
	})
	return x
}

// keyTemplateCall and reasonTemplateCall name the Site an unread template
// call is.
const (
	keyTemplateCall    = "template-call-nonconstant"
	reasonTemplateCall = "a SQL template called with an argument that is not a constant"
)

// rewriteExprs offers every expression standing in a value position of file
// to f, and puts back what f returns. composed is true for a '+' operand and
// a fmt.Sprint* argument, where a named constant reads as its value too.
func rewriteExprs(file *ast.File, f func(x ast.Expr, composed bool) ast.Expr) {
	each := func(xs []ast.Expr, composed bool) {
		for i, x := range xs {
			xs[i] = f(x, composed)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BinaryExpr:
			composed := v.Op == token.ADD
			v.X, v.Y = f(v.X, composed), f(v.Y, composed)
		case *ast.CallExpr:
			each(v.Args, isFmtSprint(v))
		case *ast.ParenExpr:
			v.X = f(v.X, false)
		case *ast.AssignStmt:
			each(v.Rhs, false)
		case *ast.ValueSpec:
			each(v.Values, false)
		case *ast.ReturnStmt:
			each(v.Results, false)
		case *ast.CompositeLit:
			each(v.Elts, false)
		case *ast.KeyValueExpr:
			v.Value = f(v.Value, false)
		case *ast.SendStmt:
			v.Value = f(v.Value, false)
		}
		return true
	})
}
