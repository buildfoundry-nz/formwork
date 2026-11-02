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
// reachable from it. So the proof fails closed: an exported package variable, a closure, an
// io.Writer, a function used as a value, a function nothing calls, a method no type of
// the module declares, and any call this does not know all keep the text a site.
// Fields and methods of one name share a node, which only adds uses.
//
// THE DRIVER IS NEVER FOLLOWED. A call of any name in driverNames is bad even when the
// module declares a function of that name, because a wrapper around a driver is the
// shape that matters most, and a cycle of such wrappers must not read as closed.
//
// DISCLOSED LIMITS. A build-tagged file is read whichever way its tag falls, which
// only adds uses. Reflection and cgo are not followed, so a struct handed whole to a
// marshaller does not count as its fields being read. Text this calls inert is judged
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
		"Split": true, "SplitN": true, "SplitAfter": true, "Fields": true, "Cut": true, "CutPrefix": true, "CutSuffix": true},
	"bytes": {"TrimSpace": true, "ReplaceAll": true, "Join": true, "Trim": true, "TrimLeft": true, "TrimRight": true,
		"TrimPrefix": true, "TrimSuffix": true, "Split": true, "Fields": true},
	"format":   {"Source": true},
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
	writerParams  []bool
	variadic      bool
}

// flowGraph is every edge a value of the module can take.
type flowGraph struct {
	vars   map[string]bool // unexported package variables, by node
	fields *flowFields
	// writers maps a writer parameter's node to its function.
	writers map[string]string
	edges   map[string][]string
	funcs   map[string]*flowFunc
	called  map[string]bool
	values  map[string]bool
	bad     map[string]bool
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
	// closures and closureLits are the file's closures only ever called (inert_flow_shapes.go).
	closures     map[*ast.Object]string
	closureLits  map[*ast.FuncLit]string
	writerParams map[*ast.Object]string
	stack        []ast.Node
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
		values: map[string]bool{}, vars: map[string]bool{}, fields: newFlowFields(), writers: map[string]string{}, bad: map[string]bool{badFlow: true}}
	var files []*flowFile
	for _, f := range e.tree.Files {
		p := f.Path()
		if !strings.HasSuffix(p, ".go") || (!tests && strings.HasSuffix(p, "_test.go")) {
			continue
		}
		src, err := f.Content()
		if err != nil {
			continue
		}
		if strings.HasSuffix(p, ".pb.go") {
			g.declareGenerated(p, src)
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			continue
		}
		ff := &flowFile{g: g, e: e, cf: newConstFile(p, file), fset: fset, file: file, path: p,
			builders: map[*ast.Object]bool{}, osFiles: map[*ast.Object]bool{},
			closures: map[*ast.Object]string{}, closureLits: map[*ast.FuncLit]string{}, writerParams: map[*ast.Object]string{}}
		ff.declare()
		ff.declareFields()
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
	for node, fn := range g.writers {
		if !g.called[fn] || g.values[fn] {
			g.edge(node, badFlow)
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
			ff.passWriters(v, fn)
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
	if id.Obj == nil || ff.isFileScope(id.Obj) {
		if key := ff.varKey(id.Name); ff.g.vars[key] && !isDeclaration(parent, id) {
			ff.flow(key, top)
			return
		}
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
// it is in the graph.
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
	for _, node := range ff.fieldNodes(sel.Sel.Name) {
		ff.flow(node, at)
	}
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
	case *ast.FuncLit:
		key, ok := ff.closureLits[f]
		return key, ok
	case *ast.Ident:
		if f.Obj != nil && f.Obj.Kind != ast.Fun {
			key, ok := ff.closures[f.Obj]
			return key, ok
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
			if i >= 2 && p.Key == cur {
				if _, isMap := ff.litType(i - 2).(*ast.MapType); isMap {
					g.edge(from, badFlow) // a map key leaves by a range the walk does not tell from its value
					return
				}
			}
			if p.Value != cur {
				return // a field name or an array index
			}
			if i >= 2 && isTextCollection(ff.litType(i-2)) {
				i -= 2
				continue
			}
			if id, ok := p.Key.(*ast.Ident); ok {
				var lit *ast.CompositeLit
				if i >= 2 {
					lit, _ = ff.stack[i-2].(*ast.CompositeLit)
				}
				ff.writeKeyed(from, lit, id.Name)
				return
			}
			g.edge(from, badFlow)
			return
		case *ast.ParenExpr, *ast.CompositeLit, *ast.SliceExpr:
			if lit, ok := p.(*ast.CompositeLit); ok && lit.Type == cur {
				return
			}
			if lit, ok := p.(*ast.CompositeLit); ok && !isTextCollection(ff.litType(i-1)) {
				if name, ok := ff.structRow(lit, cur, ff.litType(i-1)); ok {
					ff.writeField(from, name)
					return
				}
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
			}
			return // only + yields text in Go
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
			if i < 2 {
				return
			}
			call, isCall := ff.stack[i-2].(*ast.CallExpr)
			if !isCall || call.Fun != p {
				return // text has no fields, so what x.F reads came through its own node
			}
			id, ok := cur.(*ast.Ident)
			if !ok || id.Obj == nil || !ff.builders[id.Obj] {
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
			switch p.Op {
			case token.NOT, token.SUB, token.ADD, token.XOR:
				return // none of these operates on text
			}
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
				for k, v := range p.Values {
					if v == cur && k < len(p.Names) && g.vars[ff.varKey(p.Names[k].Name)] {
						g.edge(from, ff.varKey(p.Names[k].Name))
						return
					}
				}
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

// isTransparentCall reports whether call is one of transparentFuncs.
func isTransparentCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Obj == nil && transparentFuncs[pkg.Name][sel.Sel.Name]
}

// isTextCollection reports whether a literal of type x holds text directly: a slice of
// or a map from anything to text or a collection of text.
func isTextCollection(x ast.Expr) bool {
	switch t := x.(type) {
	case *ast.ArrayType:
		return t.Len == nil && isTextElement(t.Elt)
	case *ast.MapType:
		return isTextElement(t.Value)
	}
	return false
}

func isTextElement(x ast.Expr) bool {
	return isIdent(x, "string") || isByteSlice(x) || isTextCollection(x)
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
			ff.writeSelector(from, sel)
			return
		}
		if ix, ok := lhs.(*ast.IndexExpr); ok {
			lhs = ix.X
			if sel, ok := lhs.(*ast.SelectorExpr); ok {
				ff.writeSelector(from, sel)
				return
			}
		}
		id, ok := lhs.(*ast.Ident)
		switch {
		case ok && id.Name == "_":
		case ok && id.Obj != nil && !ff.isFileScope(id.Obj):
			g.edge(from, ff.localKey(id.Obj))
		case ok && g.vars[ff.varKey(id.Name)]:
			g.edge(from, ff.varKey(id.Name))
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
	if isTransparentCall(call) {
		for _, lhs := range p.Lhs {
			to(lhs)
		}
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
			if key, ok := ff.closureLits[fn]; ok {
				ff.g.edge(from, retKey(key))
				return
			}
			ff.g.edge(from, badFlow)
			return
		case *ast.FuncDecl:
			ff.g.edge(from, retKey(ff.funcKeyOf(fn)))
			return
		}
	}
	ff.g.edge(from, badFlow)
}
