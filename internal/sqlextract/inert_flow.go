package sqlextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// inert_flow.go — TEXT THAT ONLY LANDS IN A FILE REACHES NO DRIVER.
//
// A generator composes DDL for a migration file. Nothing runs it from Go: it is written
// out, and the SQL rules judge the file it lands in. inert.go proves that for text
// consumed on the spot. This proves it for text that travels: through a return, a
// local, a parameter, a strings.Builder or a slice, across the module.
//
// A GREATEST FIXPOINT OVER THE TEXT'S USES. Every place a value can go is an edge, and
// every place this cannot follow is bad. A composition is inert when nothing bad is
// reachable from it. So the proof fails closed: a package variable, a closure, an
// io.Writer, a function used as a value, a function nothing calls, a method no type of
// the module declares, and any call this does not know all keep the text a site.
// Fields and methods of one name share a node, which only adds uses.
//
// THE DRIVER IS NEVER FOLLOWED. A call of any name in driverNames is bad even when the
// module declares a function of that name, because a wrapper around a driver is the
// shape that matters most, and a cycle of such wrappers must not read as closed.
//
// DISCLOSED LIMITS. A build-tagged file is read whichever way its tag falls, which
// only adds uses. Reflection and cgo are not followed. Text this calls inert is judged
// where it lands only if a rule scopes that file.

var driverNames = map[string]bool{
	"Exec": true, "ExecContext": true, "ExecParams": true, "Query": true, "QueryContext": true, "QueryRow": true,
	"QueryRowContext": true, "Prepare": true, "PrepareContext": true, "SendBatch": true, "Queue": true, "CopyFrom": true,
	"Run": true, "Read": true,
}

// transparentFuncs return their text argument onward, in whatever form.
var transparentFuncs = map[string]map[string]bool{
	"fmt": {"Sprintf": true, "Sprint": true, "Sprintln": true},
	"strings": {"Join": true, "TrimSpace": true, "TrimPrefix": true, "TrimSuffix": true, "Trim": true, "TrimLeft": true,
		"TrimRight": true, "ReplaceAll": true, "Replace": true, "ToLower": true, "ToUpper": true, "Repeat": true,
		"Split": true, "Fields": true},
	"bytes":    {"TrimSpace": true, "ReplaceAll": true, "Join": true},
	"filepath": {"Join": true, "Base": true, "Dir": true},
	"path":     {"Join": true, "Base": true, "Dir": true},
}

// pathFuncs take text as a path and return no text of it.
var pathFuncs = map[string]map[string]bool{
	"os":      {"ReadFile": true, "Stat": true, "Lstat": true, "MkdirAll": true, "Mkdir": true, "Remove": true, "RemoveAll": true, "Open": true},
	"utf8":    {"RuneCountInString": true, "ValidString": true, "RuneCount": true, "DecodeRuneInString": true, "DecodeLastRuneInString": true},
	"sha256":  {"Sum256": true},
	"sort":    {"Strings": true},
	"parser":  {"ParseFile": true, "ParseExpr": true},
	"slices":  {"Sort": true, "Contains": true, "Equal": true, "Index": true},
	"strconv": {"Atoi": true, "ParseInt": true, "ParseBool": true, "ParseFloat": true},
}

// regexpConsuming and regexpTransparent are *regexp.Regexp's methods by what they do
// with their text. No driver spells them.
var (
	regexpConsuming = map[string]bool{"MatchString": true, "Match": true, "FindStringIndex": true, "FindAllStringIndex": true,
		"FindStringSubmatchIndex": true, "FindAllStringSubmatchIndex": true, "FindIndex": true}
	regexpTransparent = map[string]bool{"FindString": true, "FindAllString": true, "FindStringSubmatch": true,
		"FindAllStringSubmatch": true, "ReplaceAllString": true, "ReplaceAllLiteralString": true, "ReplaceAll": true}
)

const badFlow = "bad"

// flowFunc is a package-level function of the module.
type flowFunc struct {
	stringResults []int
	params        []string // the local key of each parameter, "" when unnamed
	builderParams []bool
	variadic      bool
}

// flowGraph is every edge a value of the module can take.
type flowGraph struct {
	edges  map[string][]string
	funcs  map[string]*flowFunc
	called map[string]bool
	values map[string]bool
	bad    map[string]bool
}

// flowFile is one file during the walk.
type flowFile struct {
	g        *flowGraph
	e        *constEnv
	cf       *constFile
	fset     *token.FileSet
	file     *ast.File
	path     string
	builders map[*ast.Object]bool
	osFiles  map[*ast.Object]bool
	stack    []ast.Node
}

// inertFlow is the graph a composition in path is judged on, built on first use. A
// program file's text is judged on the program, without its tests: a test that runs or
// stores generated text is not a path to a driver in what ships. A test file's text
// is judged with the tests, where its own uses are.
func (e *constEnv) inertFlow(path string) *flowGraph {
	if e == nil {
		return nil
	}
	if strings.HasSuffix(path, "_test.go") {
		e.flowOnce.Do(func() { e.flow = e.buildFlow(true) })
		return e.flow
	}
	e.programFlowOnce.Do(func() { e.programFlow = e.buildFlow(false) })
	return e.programFlow
}

// inert reports whether the composition at [pos, end) of path reaches nothing bad.
func (g *flowGraph) inert(path string, pos, end int) bool {
	if g == nil {
		return false
	}
	key := flowSiteKey(path, pos, end)
	_, known := g.edges[key]
	return known && !g.bad[key]
}

func flowSiteKey(path string, pos, end int) string {
	return "site " + path + ":" + strconv.Itoa(pos) + "-" + strconv.Itoa(end)
}

func (e *constEnv) buildFlow(tests bool) *flowGraph {
	g := &flowGraph{edges: map[string][]string{}, funcs: map[string]*flowFunc{}, called: map[string]bool{},
		values: map[string]bool{}, bad: map[string]bool{badFlow: true}}
	var files []*flowFile
	for _, f := range e.tree.Files {
		p := f.Path()
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".pb.go") || (!tests && strings.HasSuffix(p, "_test.go")) {
			continue
		}
		src, err := f.Content()
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			continue
		}
		ff := &flowFile{g: g, e: e, cf: newConstFile(p, file), fset: fset, file: file, path: p,
			builders: map[*ast.Object]bool{}, osFiles: map[*ast.Object]bool{}}
		ff.declare()
		files = append(files, ff)
	}
	for _, ff := range files {
		ff.walk()
	}
	g.settle()
	return g
}

func (g *flowGraph) edge(from, to string) { g.edges[from] = append(g.edges[from], to) }

func retKey(fn string) string          { return "ret " + fn }
func paramKey(fn string, i int) string { return "param " + fn + " " + strconv.Itoa(i) }
func (ff *flowFile) localKey(o *ast.Object) string {
	return "local " + ff.path + ":" + strconv.Itoa(ff.fset.Position(o.Pos()).Offset) + ":" + o.Name
}

// settle marks everything that reaches something bad.
func (g *flowGraph) settle() {
	for fn := range g.funcs {
		if !g.called[fn] || g.values[fn] {
			g.edge(retKey(fn), badFlow)
		}
	}
	reverse := map[string][]string{}
	for from, tos := range g.edges {
		for _, to := range tos {
			reverse[to] = append(reverse[to], from)
		}
	}
	queue := []string{badFlow}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, from := range reverse[n] {
			if !g.bad[from] {
				g.bad[from] = true
				queue = append(queue, from)
			}
		}
	}
}

// declare records the file's package-level functions, its builders and its open files.
func (ff *flowFile) declare() {
	for _, d := range ff.file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		key := ff.funcKeyOf(fd)
		if _, again := ff.g.funcs[key]; again && fd.Recv != nil {
			// Methods of one name share a node, so their parameters must line up.
			if f := ff.g.funcs[key]; len(f.params) != paramCount(fd) {
				ff.g.edge(retKey(key), badFlow)
				for i := range f.params {
					ff.g.edge(paramKey(key, i), badFlow)
				}
			}
		}
		fn := &flowFunc{}
		if fd.Type.Results != nil {
			i := 0
			for _, r := range fd.Type.Results.List {
				n := max(len(r.Names), 1)
				for k := 0; k < n; k++ {
					if isIdent(r.Type, "string") || isByteSlice(r.Type) {
						fn.stringResults = append(fn.stringResults, i)
					}
					i++
				}
			}
		}
		for _, p := range fd.Type.Params.List {
			_, fn.variadic = p.Type.(*ast.Ellipsis)
			builder := isTextBuilderType(p.Type)
			if len(p.Names) == 0 {
				fn.params = append(fn.params, "")
				fn.builderParams = append(fn.builderParams, builder)
			}
			for _, name := range p.Names {
				if name.Name == "_" || name.Obj == nil {
					fn.params = append(fn.params, "")
				} else {
					fn.params = append(fn.params, ff.localKey(name.Obj))
					if builder {
						ff.builders[name.Obj] = true
					}
				}
				fn.builderParams = append(fn.builderParams, builder)
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

// funcKeyOf is fd's node: its directory and name, or for a method, its name alone. A
// call does not say which type's method it reaches, so every method of a name shares
// one node, and a value reaching any of them reaches all of them.
func (ff *flowFile) funcKeyOf(fd *ast.FuncDecl) string {
	if fd.Recv != nil {
		return "method." + fd.Name.Name
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

// fieldKey is a field's node. An unexported field can only be touched in its own
// package, so it shares a node with that package's fields of its name; an exported one
// shares a node across the module.
func (ff *flowFile) fieldKey(name string) string {
	if ast.IsExported(name) {
		return "field " + name
	}
	return "field " + ff.cf.dir + "." + name
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

// walk adds every value's edges.
func (ff *flowFile) walk() {
	ast.Inspect(ff.file, func(n ast.Node) bool {
		if n == nil {
			ff.stack = ff.stack[:len(ff.stack)-1]
			return true
		}
		ff.stack = append(ff.stack, n)
		ff.visit(n)
		return true
	})
}

func (ff *flowFile) visit(n ast.Node) {
	top := len(ff.stack) - 1
	switch v := n.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			ff.flow(ff.site(v), top)
		}
	case *ast.CallExpr:
		if isFmtSprint(v) {
			ff.flow(ff.site(v), top)
		}
		if fn, ok := ff.calledFunc(v); ok {
			ff.g.called[fn] = true
			ff.flow(retKey(fn), top)
			ff.flow(ff.site(v), top)
		}
	case *ast.Ident:
		ff.visitIdent(v, top)
	}
}

func (ff *flowFile) site(x ast.Expr) string {
	return flowSiteKey(ff.path, ff.fset.Position(x.Pos()).Offset, ff.fset.Position(x.End()).Offset)
}

func (ff *flowFile) visitIdent(id *ast.Ident, top int) {
	parent := ff.parent(top)
	if call, ok := parent.(*ast.CallExpr); ok && call.Fun == id {
		return
	}
	if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel == id {
		ff.noteFuncValue(sel, top-1)
		ff.readField(sel, top-1)
		return
	}
	if id.Obj == nil || id.Obj.Kind == ast.Fun {
		if _, known := ff.g.funcs[ff.cf.dir+"."+id.Name]; known && isValuePosition(parent, id) {
			ff.g.values[ff.cf.dir+"."+id.Name] = true
		}
		return
	}
	if id.Obj.Kind != ast.Var || isDeclaration(parent, id) || ff.isFileScope(id.Obj) {
		return
	}
	ff.flow(ff.localKey(id.Obj), top)
}

// readField follows x.Name, unless it is the callee of a call or the target of a
// write. A Go field is only ever read through a selector like this, so every read of
// it is in the graph. Fields of one name share a node.
func (ff *flowFile) readField(sel *ast.SelectorExpr, at int) {
	switch p := ff.parent(at).(type) {
	case *ast.CallExpr:
		if p.Fun == sel {
			return
		}
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			if lhs == sel {
				return
			}
		}
	}
	ff.flow(ff.fieldKey(sel.Sel.Name), at)
}

// noteFuncValue marks pkg.F used other than by calling it.
func (ff *flowFile) noteFuncValue(sel *ast.SelectorExpr, at int) {
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Obj != nil {
		return
	}
	if call, ok := ff.parent(at).(*ast.CallExpr); ok && call.Fun == sel {
		return
	}
	if ff.e.importTable(ff.cf, pkg.Name) != nil {
		ff.g.values[ff.cf.importDirs[pkg.Name]+"."+sel.Sel.Name] = true
	}
}

func isValuePosition(parent ast.Node, id *ast.Ident) bool {
	switch p := parent.(type) {
	case *ast.FuncDecl:
		return false
	case *ast.KeyValueExpr:
		return p.Key != id
	case *ast.Field:
		return false
	case *ast.ValueSpec, *ast.AssignStmt, *ast.ReturnStmt, *ast.CallExpr, *ast.CompositeLit, *ast.BinaryExpr, *ast.ParenExpr:
		return true
	}
	return false
}

func isDeclaration(parent ast.Node, id *ast.Ident) bool {
	switch p := parent.(type) {
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			if lhs == id {
				return p.Tok == token.DEFINE || p.Tok == token.ASSIGN
			}
		}
	case *ast.ValueSpec:
		for _, name := range p.Names {
			if name == id {
				return true
			}
		}
	case *ast.Field:
		return true
	case *ast.RangeStmt:
		return p.Key == id || p.Value == id
	}
	return false
}

func (ff *flowFile) isFileScope(o *ast.Object) bool {
	return ff.file.Scope != nil && ff.file.Scope.Objects[o.Name] == o
}

func (ff *flowFile) parent(i int) ast.Node {
	if i < 1 {
		return nil
	}
	return ff.stack[i-1]
}

// calledFunc is the module function call calls, keyed by directory and name.
func (ff *flowFile) calledFunc(call *ast.CallExpr) (string, bool) {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		if f.Obj != nil && f.Obj.Kind != ast.Fun {
			return "", false
		}
		key := ff.cf.dir + "." + f.Name
		_, ok := ff.g.funcs[key]
		return key, ok
	case *ast.SelectorExpr:
		pkg, isIdent := f.X.(*ast.Ident)
		if isIdent && pkg.Obj == nil {
			if ff.e.importTable(ff.cf, pkg.Name) != nil {
				key := ff.cf.importDirs[pkg.Name] + "." + f.Sel.Name
				_, known := ff.g.funcs[key]
				return key, known
			}
			return "", false // another module's function
		}
		key := "method." + f.Sel.Name
		_, known := ff.g.funcs[key]
		return key, known && !driverNames[f.Sel.Name]
	}
	return "", false
}

// flow adds the edges of from, the value of the expression at stack index i.
func (ff *flowFile) flow(from string, i int) {
	g := ff.g
	if _, seen := g.edges[from]; !seen {
		g.edges[from] = nil
	}
	for i >= 1 {
		cur, parent := ff.stack[i], ff.stack[i-1]
		switch p := parent.(type) {
		case *ast.KeyValueExpr:
			if p.Value != cur {
				return // a map key or a field name
			}
			if i >= 2 {
				if lit, ok := ff.stack[i-2].(*ast.CompositeLit); ok && isTextCollection(lit.Type) {
					i -= 2
					continue
				}
			}
			if id, ok := p.Key.(*ast.Ident); ok {
				g.edge(from, ff.fieldKey(id.Name))
				return
			}
			g.edge(from, badFlow)
			return
		case *ast.ParenExpr, *ast.CompositeLit, *ast.SliceExpr:
			if lit, ok := p.(*ast.CompositeLit); ok && lit.Type == cur {
				return
			}
			if lit, ok := p.(*ast.CompositeLit); ok && !isTextCollection(lit.Type) {
				g.edge(from, badFlow)
				return
			}
			i--
			continue
		case *ast.BinaryExpr:
			switch p.Op {
			case token.ADD:
				i--
				continue
			case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
				return
			}
			g.edge(from, badFlow)
			return
		case *ast.IndexExpr:
			if p.Index == cur {
				return
			}
			i--
			continue
		case *ast.CallExpr:
			if p.Fun == cur {
				g.edge(from, badFlow)
				return
			}
			next, done := ff.flowThroughCall(from, p, cur)
			if done {
				return
			}
			i = next(i)
			continue
		case *ast.SelectorExpr:
			id, ok := cur.(*ast.Ident)
			if !ok || id.Obj == nil || !ff.builders[id.Obj] || i < 2 {
				g.edge(from, badFlow)
				return
			}
			call, ok := ff.stack[i-2].(*ast.CallExpr)
			if !ok || call.Fun != p {
				g.edge(from, badFlow)
				return
			}
			switch p.Sel.Name {
			case "String", "Bytes":
				i -= 2
				continue
			case "Len", "Reset", "Grow", "Cap", "Truncate", "WriteString", "WriteByte", "WriteRune", "Write":
				return
			}
			g.edge(from, badFlow)
			return
		case *ast.UnaryExpr:
			if p.Op != token.AND || i < 2 {
				g.edge(from, badFlow)
				return
			}
			id, ok := cur.(*ast.Ident)
			call, isCall := ff.stack[i-2].(*ast.CallExpr)
			if !ok || id.Obj == nil || !ff.builders[id.Obj] || !isCall {
				g.edge(from, badFlow)
				return
			}
			ff.passBuilder(from, call, p, id)
			return
		case *ast.AssignStmt:
			ff.flowIntoAssign(from, p, cur)
			return
		case *ast.ValueSpec:
			if !ff.inFunction(i) {
				g.edge(from, badFlow)
				return
			}
			for k, v := range p.Values {
				if v == cur && k < len(p.Names) && p.Names[k].Obj != nil {
					g.edge(from, ff.localKey(p.Names[k].Obj))
					return
				}
			}
			g.edge(from, badFlow)
			return
		case *ast.ReturnStmt:
			ff.flowIntoReturn(from, i)
			return
		case *ast.RangeStmt:
			if p.X == cur {
				if id, ok := p.Value.(*ast.Ident); ok && id.Obj != nil && id.Name != "_" {
					g.edge(from, ff.localKey(id.Obj))
				}
				return
			}
			return
		case *ast.ExprStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.CaseClause:
			return
		default:
			g.edge(from, badFlow)
			return
		}
	}
}

// isTextCollection reports whether a literal of type x holds text directly: []string
// or a map from anything to string.
func isTextCollection(x ast.Expr) bool {
	switch t := x.(type) {
	case *ast.ArrayType:
		return t.Len == nil && isIdent(t.Elt, "string")
	case *ast.MapType:
		return isIdent(t.Value, "string")
	}
	return false
}

func (ff *flowFile) inFunction(i int) bool {
	for ; i >= 0; i-- {
		if _, ok := ff.stack[i].(*ast.FuncDecl); ok {
			return true
		}
		if _, ok := ff.stack[i].(*ast.FuncLit); ok {
			return true
		}
	}
	return false
}

func (ff *flowFile) flowIntoAssign(from string, p *ast.AssignStmt, cur ast.Node) {
	g := ff.g
	to := func(lhs ast.Expr) {
		if sel, ok := lhs.(*ast.SelectorExpr); ok {
			g.edge(from, ff.fieldKey(sel.Sel.Name))
			return
		}
		if ix, ok := lhs.(*ast.IndexExpr); ok {
			lhs = ix.X
			if sel, ok := lhs.(*ast.SelectorExpr); ok {
				g.edge(from, ff.fieldKey(sel.Sel.Name))
				return
			}
		}
		id, ok := lhs.(*ast.Ident)
		switch {
		case ok && id.Name == "_":
		case ok && id.Obj != nil && !ff.isFileScope(id.Obj):
			g.edge(from, ff.localKey(id.Obj))
		default:
			g.edge(from, badFlow)
		}
	}
	if len(p.Lhs) == len(p.Rhs) {
		for k, r := range p.Rhs {
			if r == cur {
				to(p.Lhs[k])
			}
		}
		return
	}
	if _, ok := cur.(*ast.IndexExpr); ok && len(p.Lhs) == 2 {
		to(p.Lhs[0]) // v, ok := m[k]
		return
	}
	call, ok := cur.(*ast.CallExpr)
	if !ok {
		g.edge(from, badFlow)
		return
	}
	fn, known := ff.calledFunc(call)
	if !known {
		g.edge(from, badFlow)
		return
	}
	for _, k := range g.funcs[fn].stringResults {
		if k < len(p.Lhs) {
			to(p.Lhs[k])
		}
	}
}

func (ff *flowFile) flowIntoReturn(from string, i int) {
	for ; i >= 0; i-- {
		switch fn := ff.stack[i].(type) {
		case *ast.FuncLit:
			ff.g.edge(from, badFlow)
			return
		case *ast.FuncDecl:
			ff.g.edge(from, retKey(ff.funcKeyOf(fn)))
			return
		}
	}
	ff.g.edge(from, badFlow)
}
