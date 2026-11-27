package sqlextract

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
)

// inert_flow_methods.go — A METHOD CALL REACHES ITS RECEIVER'S METHOD.
//
// A method's node is its receiver type's, keyed by the directory declaring the type,
// the type's name and the method's name. Two types' methods of one name are two
// functions, and text one of them lands in a file says nothing of what the other does
// with its own arguments.
//
// A call whose receiver is an identifier declared with a spelled type, or a literal of
// one, reaches that type's method when the type is a package-level type of this
// package or of a package the walk holds, and declares the method itself.
//
// Any other call cannot be placed: a receiver of an interface type, a promoted method,
// a receiver the walk cannot type. Such a call reaches every method of its name that
// could take its arguments, through one dispatch node per name and call shape. Go
// checks a call's argument count against its callee's parameters, so a method that
// could not take the call is not among them; anything else is, which only adds uses.
// A call naming no method the module declares is still an unknown call, and bad.

// methodKey is the node of the method name of the type typed, "dir.Type".
func methodKey(typed, name string) string { return "method." + typed + "." + name }

// noteMethod indexes a method's node by its name, once.
func (g *flowGraph) noteMethod(name, key string) {
	for _, k := range g.methods[name] {
		if k == key {
			return
		}
	}
	g.methods[name] = append(g.methods[name], key)
}

// receiverTypeName is the name of the type a method's receiver spells.
func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	if id, ok := unwrapTypeName(recv.List[0].Type).(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// unwrapTypeName strips a pointer, parentheses and type arguments from a type.
func unwrapTypeName(x ast.Expr) ast.Expr {
	for {
		switch t := x.(type) {
		case *ast.StarExpr:
			x = t.X
		case *ast.ParenExpr:
			x = t.X
		case *ast.IndexExpr:
			x = t.X
		case *ast.IndexListExpr:
			x = t.X
		default:
			return x
		}
	}
}

// calledMethod is the node of the method call calls through f.
func (ff *flowFile) calledMethod(call *ast.CallExpr, f *ast.SelectorExpr) (string, bool) {
	// A driver's name is never followed. flowThroughCall and passBuilder refuse a key
	// ending in one as well, so text reaching a driver is bad without this; it keeps a
	// driver's own node out of the graph.
	if driverNames[f.Sel.Name] {
		return "", false
	}
	if typed := ff.receiverType(f.X); typed != "" {
		key := methodKey(typed, f.Sel.Name)
		if _, ok := ff.g.funcs[key]; ok {
			return key, true
		}
	}
	return ff.g.dispatch(f.Sel.Name, call)
}

// receiverType is "dir.Type" for the named type x was declared with or spelled as, or
// "" when the walk cannot name it.
func (ff *flowFile) receiverType(x ast.Expr) string {
	for {
		p, ok := x.(*ast.ParenExpr)
		if !ok {
			break
		}
		x = p.X
	}
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		x = u.X
	}
	var typ ast.Expr
	switch v := x.(type) {
	case *ast.Ident:
		typ = ff.spelledTypeOf(v)
	case *ast.CompositeLit:
		typ = v.Type
	}
	if typ == nil {
		return ""
	}
	switch t := unwrapTypeName(typ).(type) {
	case *ast.Ident:
		// A name this file resolves to anything but its own package-level type is a
		// type parameter or a function's local type, never a type with methods here.
		if t.Obj != nil && (t.Obj.Kind != ast.Typ || !ff.isFileScope(t.Obj)) {
			return ""
		}
		return ff.cf.dir + "." + t.Name
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok || pkg.Obj != nil {
			return ""
		}
		ff.e.importTable(ff.cf, pkg.Name)
		if dir, ok := ff.cf.importDirs[pkg.Name]; ok {
			return dir + "." + t.Sel.Name
		}
	}
	return ""
}

// dispatch is the node of a call of a method name whose receiver the walk cannot place:
// every method of that name the call's arguments fit. One fitting method is its own
// node; several meet at a dispatch node keyed by the name and the call's shape, whose
// parameter j reaches each one's parameters that argument j can fill, and whose return
// is each one's return. What a fitting method writes to a builder parameter comes back
// by its own node, so text given to one fitting method never reaches another through
// this one.
func (g *flowGraph) dispatch(name string, call *ast.CallExpr) (string, bool) {
	n, ellipsis := len(call.Args), call.Ellipsis != token.NoPos
	_, spread := singleCallArg(call)
	type fit struct {
		key  string
		into [][]int // the parameters argument j fills
	}
	var fits []fit
	for _, c := range g.methods[name] {
		if into := argsInto(g.funcs[c], n, ellipsis, spread); into != nil {
			fits = append(fits, fit{key: c, into: into})
		}
	}
	switch len(fits) {
	case 0:
		return "", false
	case 1:
		if !spread {
			return fits[0].key, true // the call's own arguments line up with its parameters
		}
	}
	key := "method.#" + strconv.Itoa(n)
	if ellipsis {
		key += "..."
	}
	if spread {
		key += "*"
	}
	key += "." + name
	if _, done := g.funcs[key]; done {
		return key, true
	}
	d := &flowFunc{params: make([]string, n), back: make([]string, n), builderParams: make([]bool, n), writerParams: make([]bool, n), plainParams: make([]bool, n)}
	results := map[int]bool{}
	for j := 0; j < n; j++ {
		node, back := "dispatch "+key+" "+strconv.Itoa(j), "dispatch back "+key+" "+strconv.Itoa(j)
		d.params[j], d.back[j] = node, back
		g.edge(paramKey(key, j), node)
		builders, writers, plain := 0, 0, true
		reached := 0
		for _, f := range fits {
			c := g.funcs[f.key]
			for _, k := range f.into[j] {
				reached++
				if c.builderParams[k] {
					builders++
				}
				if c.writerParams[k] {
					writers++
					g.edge(writerKey(f.key, k), writerKey(key, j))
				}
				plain = plain && c.plainParams[k]
				g.edge(node, paramKey(f.key, k))
				if c.params[k] == "" {
					g.edge(node, badFlow)
					continue
				}
				if c.builderParams[k] {
					g.edge(c.params[k], back) // what the method writes to the caller's builder
				}
			}
		}
		if (builders != 0 && builders != reached) || (writers != 0 && writers != reached) {
			// The fitting methods take this argument as different things, and the call
			// is followed as one of them: whatever the others write to it is lost.
			g.edge(node, badFlow)
			g.edge(back, badFlow)
			g.edge(writerKey(key, j), badFlow)
		}
		d.builderParams[j], d.writerParams[j], d.plainParams[j] = builders != 0, writers != 0, plain
	}
	for _, f := range fits {
		g.edge(retKey(f.key), retKey(key))
		for _, r := range g.funcs[f.key].stringResults {
			results[r] = true
		}
	}
	for r := range results {
		d.stringResults = append(d.stringResults, r)
	}
	slices.Sort(d.stringResults)
	g.funcs[key] = d
	for _, f := range fits {
		g.dispatches[key] = append(g.dispatches[key], f.key)
	}
	return key, true
}

// argsInto is, for each of a call's n arguments, the parameters of fn it fills, or nil
// when fn cannot take the call. A lone call argument may be a call of several results,
// which fill every parameter.
func argsInto(fn *flowFunc, n int, ellipsis, spread bool) [][]int {
	p := len(fn.params)
	into := make([][]int, n)
	switch {
	case ellipsis:
		if !fn.variadic || p != n {
			return nil
		}
	case spread && (p > 1 || fn.variadic):
		into[0] = make([]int, p)
		for k := range into[0] {
			into[0][k] = k
		}
		return into
	case fn.variadic:
		if n < p-1 {
			return nil
		}
	default:
		if p != n {
			return nil
		}
	}
	for j := range into {
		into[j] = []int{min(j, p-1)}
	}
	return into
}

// singleCallArg reports whether call's only argument is itself a call.
func singleCallArg(call *ast.CallExpr) (*ast.CallExpr, bool) {
	if len(call.Args) != 1 || call.Ellipsis != token.NoPos {
		return nil, false
	}
	inner, ok := call.Args[0].(*ast.CallExpr)
	return inner, ok
}
