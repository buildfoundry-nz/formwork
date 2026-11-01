package sqlextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// consts.go — GO STRING CONSTANTS ARE READ, WHEREVER THEY ARE DECLARED.
//
// Constant folding was always the extractors' boundary, and it stopped at the
// literal: "SELECT " + cols + " FROM t" was a composition neither could read
// when cols was a constant, so the query was not judged and the census listed
// it. A query composed from named constants is the ordinary shape, not an
// exotic one — a shared CTE, a column list, a status predicate kept in one
// place and spliced into every statement that needs it. Measured on one
// corpus, 900 of the 2,040 SQL compositions its SQL rules govern were made of
// nothing but literals and constants: 501 from constants of their own package,
// the rest from constants another package of the module exports.
//
// The boundary is now the Go language's own: a constant expression of string
// type. A literal, a named constant (in this file, another file of the
// package, or an imported package of the module), a '+' of those, a
// parenthesis, and a conversion of one to a string type. Anything else — a
// variable, a call, a field — is what the Go compiler would not fold either,
// and it stays a composition the extractor cannot read.
//
// HOW: A PASS OVER THE AST BEFORE THE EXTRACTORS WALK IT. Each operand of a '+'
// and each argument of fmt.Sprint{,f,ln} that names a string constant is
// replaced by a parenthesised literal holding its value, spanning the operand's
// own source range. Every extractor arm already reads a literal there, so none
// of them changes, and a fragment's Partial marking and a candidate's line stay
// what the source says. A bare constant (pool.Query(ctx, listSQL)) is left
// alone: its declaration is already a candidate, and a second one at the use
// would judge the same text twice. The replacements are remembered so neither
// extractor collects one as a candidate of its own.
//
// WHERE NAMES RESOLVE. In the file, through the parser's scopes, so a local
// shadowing a package constant is the local. Elsewhere in the package, through
// the walk (scan.File.Tree) — a name the file does not declare is a package
// name or an import. In an imported package, when the module's go.mod (its
// module path, or a replace directive to a directory in the tree) places the
// import in the walk. A name declared more than once in a package (two files
// under different build constraints) is not read: which one the build sees is
// not a fact the extractor has. Without a tree (scan.NewMemFile) only the
// file's own constants are read.
//
// RESIDUAL: go.work, a vendor directory and a module outside the tree are not
// read, so their constants stay operands the extractor cannot read. That
// fails toward the census, which lists the composition, never toward a pass.

// constEnvKey is the scan.FileSet.Memo key of a walk's constEnv.
type constEnvKey struct{}

// constEnv is one walk's constant tables, built per package as first needed.
type constEnv struct {
	tree *scan.FileSet

	mu    sync.Mutex
	pkgs  map[pkgKey]*pkgEntry
	mods  map[string]*goMod // by module root dir
	names map[string]string // package name by dir, "" when none

	usesOnce sync.Once
	uses     map[templateKey]*templateUse // template_index.go
}

type pkgKey struct {
	dir, name string
	tests     bool // the package's _test.go files are part of it
}

type pkgEntry struct {
	once sync.Once
	t    *pkgTable
}

// pkgTable is one package's string constants and string types.
type pkgTable struct {
	// decls counts top-level declarations of each name, of any kind. A
	// constant is read only when its name is declared once.
	decls  map[string]int
	consts map[string]constDef
	// strTypes are the types declared as `type T string`, which a conversion
	// at a use site may name.
	strTypes map[string]bool
	// templates are the package's SQL template functions (templates.go).
	templates map[string]funcDef
}

type funcDef struct {
	decl *ast.FuncDecl
	file *constFile
}

type constDef struct {
	expr ast.Expr // nil for a spec with no value of its own (iota repetition)
	file *constFile
}

// constFile is what folding a declaration needs of the file declaring it: its
// directory and package (for its own package's names) and its imports.
type constFile struct {
	path, dir, pkg string
	test           bool
	imports        map[string]string // local name → import path, aliased only
	unaliased      []string          // import paths named by their package clause
	importDirs     map[string]string // local name → dir, resolved on first use
	importsOnce    sync.Once
}

func envFor(tree *scan.FileSet) *constEnv {
	if tree == nil {
		return nil
	}
	return tree.Memo(constEnvKey{}, func() any {
		return &constEnv{tree: tree, pkgs: map[pkgKey]*pkgEntry{}, mods: map[string]*goMod{}, names: map[string]string{}}
	}).(*constEnv)
}

// newConstFile describes file, parsed from the repo-relative path p.
func newConstFile(p string, file *ast.File) *constFile {
	cf := &constFile{path: p, dir: path.Dir(p), pkg: file.Name.Name, test: strings.HasSuffix(p, "_test.go"), imports: map[string]string{}}
	for _, is := range file.Imports {
		ip, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		switch {
		case is.Name == nil:
			cf.unaliased = append(cf.unaliased, ip)
		case is.Name.Name == "_" || is.Name.Name == ".":
		default:
			cf.imports[is.Name.Name] = ip
		}
	}
	return cf
}

// table returns the constants of the package named name in dir.
func (e *constEnv) table(k pkgKey) *pkgTable {
	e.mu.Lock()
	ent := e.pkgs[k]
	if ent == nil {
		ent = &pkgEntry{}
		e.pkgs[k] = ent
	}
	e.mu.Unlock()
	ent.once.Do(func() { ent.t = e.build(k) })
	return ent.t
}

func (e *constEnv) build(k pkgKey) *pkgTable {
	t := &pkgTable{decls: map[string]int{}, consts: map[string]constDef{}, strTypes: map[string]bool{}, templates: map[string]funcDef{}}
	for _, f := range e.tree.Dir(k.dir) {
		p := f.Path()
		if !strings.HasSuffix(p, ".go") || (!k.tests && strings.HasSuffix(p, "_test.go")) {
			continue
		}
		src, err := f.Content()
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, src, 0)
		if err != nil || file.Name.Name != k.name {
			continue
		}
		cf := newConstFile(p, file)
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					t.decls[d.Name.Name]++
					if isTemplate(d) {
						t.templates[d.Name.Name] = funcDef{decl: d, file: cf}
					}
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.ValueSpec:
						for i, n := range sp.Names {
							t.decls[n.Name]++
							if d.Tok != token.CONST {
								continue
							}
							def := constDef{file: cf}
							if i < len(sp.Values) && len(sp.Names) == len(sp.Values) {
								def.expr = sp.Values[i]
							}
							t.consts[n.Name] = def
						}
					case *ast.TypeSpec:
						t.decls[sp.Name.Name]++
						if id, ok := sp.Type.(*ast.Ident); ok && id.Name == "string" && sp.Assign == 0 && sp.TypeParams == nil {
							t.strTypes[sp.Name.Name] = true
						}
					}
				}
			}
		}
	}
	return t
}

// ownTable is the table a name the file does not declare resolves in: its own
// package, with the package's test files when it is one.
func (e *constEnv) ownTable(cf *constFile) *pkgTable {
	return e.table(pkgKey{dir: cf.dir, name: cf.pkg, tests: cf.test})
}

// importTable is the table of the package cf imports as local, or nil.
func (e *constEnv) importTable(cf *constFile, local string) *pkgTable {
	cf.importsOnce.Do(func() {
		cf.importDirs = map[string]string{}
		for name, ip := range cf.imports {
			if dir := e.importDir(cf.dir, ip); dir != "" {
				cf.importDirs[name] = dir
			}
		}
		for _, ip := range cf.unaliased {
			dir := e.importDir(cf.dir, ip)
			if dir == "" {
				continue
			}
			if name := e.packageName(dir); name != "" {
				if _, taken := cf.importDirs[name]; !taken {
					cf.importDirs[name] = dir
				}
			}
		}
	})
	dir, ok := cf.importDirs[local]
	if !ok {
		return nil
	}
	name := e.packageName(dir)
	if name == "" {
		return nil
	}
	// A test file importing its own directory's package (the external test
	// package's import of the package under test) sees that package's test
	// files too: `go test` compiles them into it, export_test.go's reason to
	// exist.
	return e.table(pkgKey{dir: dir, name: name, tests: cf.test && dir == cf.dir})
}

// packageName is the package clause of dir's non-test Go files, or "".
func (e *constEnv) packageName(dir string) string {
	e.mu.Lock()
	name, ok := e.names[dir]
	e.mu.Unlock()
	if ok {
		return name
	}
	for _, f := range e.tree.Dir(dir) {
		p := f.Path()
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := f.Content()
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, src, parser.PackageClauseOnly)
		if err == nil {
			name = file.Name.Name
			break
		}
	}
	e.mu.Lock()
	e.names[dir] = name
	e.mu.Unlock()
	return name
}

// fold returns the value of expr when it is a Go constant expression of string
// type. inConst is true while folding a constant's own declared value, where
// a one-argument call can only be a conversion: Go admits no other call there
// that yields a string.
func (e *constEnv) fold(expr ast.Expr, cf *constFile, inConst bool, env map[string]string, depth int) (string, bool) {
	if depth > 64 {
		return "", false
	}
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return e.fold(v.X, cf, inConst, env, depth+1)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := e.fold(v.X, cf, inConst, env, depth+1)
		if !ok {
			return "", false
		}
		r, ok := e.fold(v.Y, cf, inConst, env, depth+1)
		return l + r, ok
	case *ast.Ident:
		if v.Obj != nil && v.Obj.Kind == ast.Var {
			if _, param := v.Obj.Decl.(*ast.Field); param {
				// A template's parameter, bound by the call being evaluated.
				val, ok := env[v.Name]
				return val, ok
			}
		}
		if v.Obj != nil {
			if v.Obj.Kind != ast.Con {
				return "", false
			}
			sp, ok := v.Obj.Decl.(*ast.ValueSpec)
			if !ok || len(sp.Names) != len(sp.Values) {
				return "", false
			}
			for i, n := range sp.Names {
				if n.Name == v.Name {
					return e.fold(sp.Values[i], cf, true, nil, depth+1)
				}
			}
			return "", false
		}
		if e == nil {
			return "", false
		}
		return e.foldIn(e.ownTable(cf), v.Name, depth)
	case *ast.SelectorExpr:
		x, ok := v.X.(*ast.Ident)
		if !ok || x.Obj != nil || e == nil || !ast.IsExported(v.Sel.Name) {
			return "", false
		}
		if t := e.importTable(cf, x.Name); t != nil {
			return e.foldIn(t, v.Sel.Name, depth)
		}
	case *ast.CallExpr:
		if v.Ellipsis != token.NoPos {
			return "", false
		}
		if len(v.Args) == 1 && (inConst || e.namesStringType(v.Fun, cf)) {
			return e.fold(v.Args[0], cf, inConst, env, depth+1)
		}
		if inConst {
			return "", false
		}
		return e.callTemplate(v, cf, env, depth)
	}
	return "", false
}

// foldIn folds the package-level constant name of t.
func (e *constEnv) foldIn(t *pkgTable, name string, depth int) (string, bool) {
	if t.decls[name] != 1 {
		return "", false
	}
	def, ok := t.consts[name]
	if !ok || def.expr == nil {
		return "", false
	}
	return e.fold(def.expr, def.file, true, nil, depth+1)
}

// namesStringType reports whether fun, the callee of a one-argument call at a
// use site, names a string type: string itself, or a type declared as
// `type T string` in the file, its package, or an imported package.
func (e *constEnv) namesStringType(fun ast.Expr, cf *constFile) bool {
	switch f := fun.(type) {
	case *ast.ParenExpr:
		return e.namesStringType(f.X, cf)
	case *ast.Ident:
		if f.Obj != nil {
			ts, ok := f.Obj.Decl.(*ast.TypeSpec)
			if !ok || f.Obj.Kind != ast.Typ || ts.Assign != 0 {
				return false
			}
			id, ok := ts.Type.(*ast.Ident)
			return ok && id.Name == "string" && id.Obj == nil
		}
		if e == nil {
			return f.Name == "string"
		}
		t := e.ownTable(cf)
		if f.Name == "string" {
			return t.decls["string"] == 0
		}
		return t.decls[f.Name] == 1 && t.strTypes[f.Name]
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		if !ok || x.Obj != nil || e == nil {
			return false
		}
		t := e.importTable(cf, x.Name)
		return t != nil && t.decls[f.Sel.Name] == 1 && t.strTypes[f.Sel.Name]
	}
	return false
}

// folded is what foldConstants did to a file: the literals it made, which
// are not candidates of their own; the spans of the file's SQL templates'
// bodies, whose compositions are judged at every call instead (templates.go);
// and the template calls it could not evaluate, which are compositions the
// extractor cannot read.
type folded struct {
	made map[ast.Expr]bool
	// bodies are the file's SQL template bodies; a call inside one takes the
	// template's parameters and is not reported on its own.
	bodies []span
	// judged are the bodies the walk instantiates in full: their compositions
	// are not sites, and instances holds one candidate per argument set.
	judged      []span
	instances   []Candidate
	unread      []Site
	unreadSpans []span
}

type span struct{ lo, hi token.Pos }

func (f folded) inTemplate(e ast.Expr) bool { return within(e, f.bodies) }

// judgedBody reports whether e is inside a template body the walk
// instantiates in full, whose compositions are judged per instance.
func (f folded) judgedBody(e ast.Expr) bool { return within(e, f.judged) }

// inUnread reports whether e is inside a template call already recorded as
// unread: an argument of that call is part of its Site.
func (f folded) inUnread(e ast.Expr) bool { return within(e, f.unreadSpans) }

func within(e ast.Expr, spans []span) bool {
	for _, s := range spans {
		if e.Pos() >= s.lo && e.End() <= s.hi {
			return true
		}
	}
	return false
}

// foldConstants is the pass the extractors run before walking file: each
// operand of a '+' and each argument of fmt.Sprint{,f,ln} that is a named
// string constant, or a conversion of one, becomes a parenthesised literal
// spanning the operand, and so does every call of a SQL template whose
// arguments are constants, wherever it stands (templates.go). tree may be nil.
func foldConstants(tree *scan.FileSet, p string, fset *token.FileSet, file *ast.File) folded {
	e := envFor(tree)
	cf := newConstFile(p, file)
	out := folded{made: map[ast.Expr]bool{}}
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !isTemplate(fd) {
			continue
		}
		body := fd.Body.List[0].(*ast.ReturnStmt).Results[0]
		sp := span{body.Pos(), body.End()}
		out.bodies = append(out.bodies, sp)
		u := e.useOf(fd, cf)
		if u == nil || u.dynamic || len(u.argSets) == 0 {
			// No walk to read; a call passing a non-constant; or no call at
			// all, so nothing says what it builds. Its compositions are sites.
			continue
		}
		var texts []string
		complete := true
		for _, set := range u.argSets {
			text, ok := e.fold(body, cf, false, set, 0)
			if !ok {
				complete = false
				break
			}
			texts = append(texts, text)
		}
		if !complete {
			continue
		}
		// Called, and only ever with constants: judged once per instance.
		out.judged = append(out.judged, sp)
		pos := fset.Position(body.Pos())
		for _, t := range texts {
			out.instances = append(out.instances, Candidate{Text: t, Line: pos.Line, Col: pos.Column})
		}
	}
	// literal stands s in for x. A constant's value is not a candidate of its
	// own — its declaration already is — but a template call's is: the
	// instance it builds is a statement made here.
	literal := func(x ast.Expr, s string) ast.Expr {
		lit := &ast.ParenExpr{
			Lparen: x.Pos(),
			X:      &ast.BasicLit{ValuePos: x.Pos(), Kind: token.STRING, Value: strconv.Quote(s)},
			Rparen: x.End() - 1,
		}
		_, call := x.(*ast.CallExpr)
		out.made[lit] = !call || e.isConversion(x.(*ast.CallExpr), cf)
		return lit
	}
	// operand is a '+' operand or a fmt.Sprint* argument: a named constant
	// there is read as its value. A bare constant is not — its declaration is
	// already the candidate.
	operand := func(x ast.Expr) ast.Expr {
		switch x.(type) {
		case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr:
		default:
			return x
		}
		if s, ok := e.fold(x, cf, false, nil, 0); ok {
			return literal(x, s)
		}
		return anywhere(e, cf, fset, x, literal, &out, false)
	}
	rewriteExprs(file, func(x ast.Expr, composed bool) ast.Expr {
		if composed {
			return operand(x)
		}
		return anywhere(e, cf, fset, x, literal, &out, true)
	})
	return out
}

// isConversion reports whether call converts one argument to a string type.
func (e *constEnv) isConversion(call *ast.CallExpr, cf *constFile) bool {
	return len(call.Args) == 1 && e.namesStringType(call.Fun, cf)
}

// isFmtSprint reports whether call is fmt.Sprint, fmt.Sprintf or fmt.Sprintln.
func isFmtSprint(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" || pkg.Obj != nil {
		return false
	}
	switch sel.Sel.Name {
	case "Sprint", "Sprintf", "Sprintln":
		return true
	}
	return false
}
