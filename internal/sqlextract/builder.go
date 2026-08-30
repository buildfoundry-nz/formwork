package sqlextract

import (
	"go/ast"
	"go/token"
	"strings"
)

// builder.go — #311. THE QUERY THE FOLD NEVER SAW AT ALL.
//
// Every other refusal in this package starts from a tracked variable: a name
// seeded with literal text, then written in a way the walk cannot model. A
// strings.Builder query has no such name. `var sb strings.Builder` binds no
// string, `sb.WriteString(…)` is a call and not an assignment, and `sb.String()`
// returns a value the fold has never modelled — so nothing is tracked, nothing
// is untracked, and the composition is invisible to the whole mechanism rather
// than dropped by it.
//
// It is a coverage limit all the same, and the one the COVERAGE LIMIT block has
// disclosed longest. Leaving it out of the census would have rebuilt exactly the
// blindness #311 is about, one shape smaller: the reviewer's four-file repo
// hides its unordered locking SELECT behind a builder in one of the four.
//
// SYNTACTIC, AND NARROW ON PURPOSE. This pass resolves no types (spec §2), so a
// builder is recognised by the two spellings that carry the type in the source:
// a declaration naming strings.Builder or bytes.Buffer, and a signature
// declaring one. A builder reached some other way — a struct field, a value
// returned by a helper — is not recognised, and the cost is the pre-#311
// silence for that one shape rather than a wrong claim about it. The direction
// matters: over-recognising would put a census line against every `.WriteString`
// on every type in the repo, which is the flood that makes a diagnostic channel
// unreadable.

// builderWriteMethods are the write calls whose literal argument contributes
// text. Read is not among them, and neither is String: this is about what text
// went IN.
var builderWriteMethods = map[string]bool{
	"WriteString": true,
	"Write":       true,
	"WriteRune":   true,
	"WriteByte":   true,
}

// builderSites records one Site per builder in body that accumulates literal
// text, anchored at its FIRST write and carrying every literal it accumulates.
//
// ONE SITE PER BUILDER, NOT PER WRITE. A builder is one query; a line per
// WriteString would report a five-line query five times and rank a verbose
// composition above a hazardous one. The first write is the anchor because it
// is where the query starts, and because it is a real write rather than the
// declaration — the declaration is where the TYPE is, which is a fact about
// plumbing rather than about SQL.
//
// THE TEXT IS THE CONCATENATION of every literal written in source order, which
// is the best reading of the query this pass can make and is used for one
// purpose: telling a SQL-bearing builder from a builder assembling a log line,
// so the caller's looksLikeSQL filter has something true to judge. It is never
// rendered — the census prints the reason, never the text — so it makes no
// claim about what the query really is. That is the whole point: if this pass
// could make that claim, the composition would not be a coverage limit.
func builderSites(sig *ast.FuncType, body *ast.BlockStmt, sink *siteSink) {
	if sink == nil || !sink.scanBuilders {
		return
	}
	names := builderParamNames(sig, builderNames(body))
	if len(names) == 0 {
		return
	}
	seeds := builderSeeds(body, sink.pkgSeeds)
	type acc struct {
		first token.Pos
		text  strings.Builder
	}
	order := []string{}
	byName := map[string]*acc{}
	// DESCENDS INTO NESTED LITERALS, which every other analysis in this package
	// refuses to do — and here refusing was a miss rather than a narrowing. A
	// builder declared in this scope and written inside a closure
	// (`f := func(){ sb.WriteString(…) }`) is invisible to the closure's own
	// fold scope, whose builderNames never sees the declaration, so stopping at
	// the literal here left it invisible to BOTH and the query was reported
	// nowhere. The double-report that stopping guarded against is a name
	// declared as a builder in two nested scopes, and the sink already collapses
	// that: both records land on one line with one key and one text.
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !builderWriteMethods[sel.Sel.Name] || len(call.Args) == 0 {
			return true
		}
		recv, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok || !names[recv.Name] {
			return true
		}
		text, real := builderOperand(call.Args[0], seeds)
		if !real {
			// A write of a pure runtime value contributes no readable text.
			// Recording the builder on the strength of it would put a site on a
			// composition this pass has no evidence is SQL.
			return true
		}
		a := byName[recv.Name]
		if a == nil {
			a = &acc{first: call.Pos()}
			byName[recv.Name] = a
			order = append(order, recv.Name)
		}
		a.text.WriteString(text)
		return true
	})
	for _, name := range order {
		a := byName[name]
		sink.add(a.first, reasonStringsBuilder, a.text.String())
	}
}

// builderOperand reads the text one builder write contributes, resolving a name
// against the literal seeds builderSeeds gathered — the enclosing body's own
// bindings, and the file's package-level declarations under them — before
// falling back to reassembleOperand.
//
// WITHOUT IT THE CENSUS'S ANSWER DEPENDED ON WHETHER THE OPERATOR INLINED THEIR
// SEED. #311 half 2 names strings.Builder first among the four shapes whose
// unordered locking SELECT the gate cannot see, and the reviewer's own repo
// wrote `q := "SELECT …"; b.WriteString(q); b.WriteString(" FOR UPDATE")`.
// reassembleOperand cannot read an identifier, so the builder accumulated
// " FOR UPDATE" alone, sqlparse.looksLikeSQL correctly judged that not to be
// SQL, and the site was dropped — while the rule reported nothing about the
// file either, the only name it tracks holding the unlocked seed. The shape the
// issue leads with stayed exactly as silent as it was before the fix.
//
// The seed is text that went INTO the builder, so the builder's text carries
// it, and that is the only use it is put to: telling a SQL-bearing builder from
// one assembling a log line or a path, per builderSites' contract that the text
// is never rendered and makes no claim about what the query really is. A name
// with no readable binding reads exactly as it did — unreal, contributing
// nothing — so a builder this pass has no evidence is SQL still gets no line.
func builderOperand(expr ast.Expr, seeds map[string]string) (string, bool) {
	switch v := expr.(type) {
	case *ast.ParenExpr:
		return builderOperand(v.X, seeds)
	case *ast.Ident:
		if s, ok := seeds[v.Name]; ok {
			return s, true
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			l, lReal := builderOperand(v.X, seeds)
			r, rReal := builderOperand(v.Y, seeds)
			return l + r, lReal || rReal
		}
	}
	return reassembleOperand(expr)
}

// builderLiteralSeeds maps each name in body to the literal text bound to it by
// a `:=`, a plain `=`, or a `var` declaration.
//
// EVERY BINDING, CONCATENATED IN SOURCE ORDER, exactly as builderSites
// concatenates every write. A name bound twice holds two different queries and
// this pass cannot say which one reaches the builder — that is the untracked
// write it is disclosing, so picking one would be the guess, and picking the
// wrong one would answer "is any SQL flowing into this builder" with silence.
// Concatenating cannot lose a SQL-shaped binding that a choice between them
// could, and it makes no claim it could get wrong: the same contract holds
// here, that the text distinguishes a SQL-bearing builder and is never
// rendered as the query.
//
// += IS NOT A BINDING. Its right-hand side is one append and not the value, so
// reading it as a seed would call a builder SQL-bearing on the strength of a
// fragment, and the composed value of an appended-to name is the fold's
// business — where the fold cannot model it, the name is untracked and carries
// its own site.
//
// Keyed by bare name with no scope stack, as builderNames and fixedArrays are,
// and it descends into closures for the reason builderSites does: a builder
// declared here and written inside a `func(){…}` is reached by that walk, so
// the seeds written there have to be reachable too. A shadowed name therefore
// reads as both bindings joined, which costs the SQL-ness judgement for one
// builder and never a claim about a composition the rule read.
func builderLiteralSeeds(body *ast.BlockStmt) map[string]string {
	var out map[string]string
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if i < len(x.Values) {
					out = bindSeed(out, id, x.Values[i])
				}
			}
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE && x.Tok != token.ASSIGN {
				return true
			}
			// Paired by index, so a multi-value call on the right — one
			// expression for several names — binds nothing.
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, lhs := range x.Lhs {
				out = bindSeed(out, lhs, x.Rhs[i])
			}
		}
		return true
	})
	return out
}

// bindSeed records lhs as holding rhs's readable literal text, appending to any
// text the name already carries. Returns the map because it allocates lazily:
// most bodies bind no readable literal at all, and a builder file is walked per
// function.
func bindSeed(out map[string]string, lhs, rhs ast.Expr) map[string]string {
	name, ok := ast.Unparen(lhs).(*ast.Ident)
	if !ok {
		return out
	}
	text, real := reassembleOperand(rhs)
	if !real {
		return out
	}
	if out == nil {
		out = map[string]string{}
	}
	out[name.Name] += text
	return out
}

// fileLiteralSeeds maps each PACKAGE-LEVEL name in file to the literal text a
// `const` or `var` declaration binds to it.
//
// A PACKAGE SCOPE IS WHERE GO CODE PUTS A QUERY, and leaving it out left #311
// half 2 open for the shape #311 leads with. The builder fix read the enclosing
// body's bindings because the reviewer's repro inlined its seed; write the same
// query the way it is usually written —
//
//	const listQ = `SELECT id FROM t WHERE s = 'x'`
//	…
//	b.WriteString(listQ)
//	b.WriteString(" FOR UPDATE")
//
// — and the builder accumulated " FOR UPDATE" alone, which is not SQL-shaped,
// so the site was dropped and the census reported nothing about a file the rule
// also reports nothing about, a builder being a composition it never tracks.
//
// TOP-LEVEL DECLARATIONS ONLY, walked off file.Decls rather than by inspecting
// the tree: a `q := …` inside some OTHER function is not in scope where the
// builder is written, and reading it here would put that function's text on
// this one's builder.
//
// THE RESIDUE IS THE REST OF THE PACKAGE. This pass reads one file (spec §2), so
// a query constant declared in a SIBLING FILE of the same package is not read,
// its builder accumulates no SQL-shaped text, and the census stays silent about
// it exactly as it did before #311 — a coverage gap of the census, disclosed
// here, and not a claim that the composition was read.
func fileLiteralSeeds(file *ast.File) map[string]string {
	var out map[string]string
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			// The spec type carries the whole filter: only a `const` or `var`
			// GenDecl holds ValueSpecs, an `import` holding ImportSpecs and a
			// `type` holding TypeSpecs. Testing gd.Tok as well would be a
			// condition no input can falsify.
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Paired by index. A spec with fewer values than names is either a
			// multi-value call binding several names at once or a `const` spec
			// repeating the previous expression implicitly, and neither carries
			// text this pass can attribute to a name.
			for i, id := range vs.Names {
				if i < len(vs.Values) {
					out = bindSeed(out, id, vs.Values[i])
				}
			}
		}
	}
	return out
}

// builderSeeds is the seed map builderOperand resolves names against: the
// enclosing body's own bindings, with the file's package-level declarations
// filling in for names the body binds nowhere.
//
// THE BODY'S BINDING WINS OUTRIGHT, never joined to the package one. Inside a
// function that writes `listQ := "/var/log/app"`, `listQ` is the local, and
// concatenating a package-level query constant of the same name onto it would
// report a SQL coverage gap in a function composing a log path — the flood this
// file's doc warns about, arriving through a name collision. Joining is right
// for two bindings in ONE scope, where this pass genuinely cannot say which
// reaches the builder; between scopes the language already says.
func builderSeeds(body *ast.BlockStmt, pkg map[string]string) map[string]string {
	local := builderLiteralSeeds(body)
	if len(pkg) == 0 {
		return local
	}
	if local == nil {
		local = map[string]string{}
	}
	for name, text := range pkg {
		if _, bound := local[name]; !bound {
			local[name] = text
		}
	}
	return local
}

// builderNames is every name in body that a declaration or the enclosing
// signature says is a strings.Builder or bytes.Buffer, by value or by pointer.
//
// Both types, because they are one shape to a reader and to this analysis: a
// value composed by write calls with no string-literal seed anywhere. Keyed by
// bare name with no scope stack, exactly as fixedArrays and closureAppends are,
// so a same-named non-builder in a sibling block is treated as the builder too —
// which costs a census line about a composition that was readable, and takes
// both a name collision and a literal-bearing write call on that name to reach.
func builderNames(body *ast.BlockStmt) map[string]bool {
	var out map[string]bool
	add := func(id *ast.Ident) {
		if out == nil {
			out = map[string]bool{}
		}
		out[id.Name] = true
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			// Its parameters and declarations belong to its own scope; foldBlock
			// visits it separately.
			return false
		case *ast.ValueSpec:
			// `var sb strings.Builder`, and `var sb = strings.Builder{}` /
			// `new(strings.Builder)` for the value spelling.
			if isBuilderType(x.Type) {
				for _, name := range x.Names {
					add(name)
				}
				return true
			}
			for i, name := range x.Names {
				if i < len(x.Values) && isBuilderValue(x.Values[i]) {
					add(name)
				}
			}
		case *ast.AssignStmt:
			// `sb := strings.Builder{}`, `sb := &strings.Builder{}`,
			// `sb := new(strings.Builder)`. Paired by index, so a call with a
			// mismatched arity contributes nothing rather than a wrong name.
			for i, lhs := range x.Lhs {
				id, ok := ast.Unparen(lhs).(*ast.Ident)
				if ok && i < len(x.Rhs) && isBuilderValue(x.Rhs[i]) {
					add(id)
				}
			}
		}
		return true
	})
	return out
}

// builderParamNames adds the names an enclosing signature declares as builders.
// A helper taking `b *strings.Builder` and appending SQL to it is the ordinary
// spelling of a composed query split across functions, and the parameter is the
// only place its type is written down.
func builderParamNames(sig *ast.FuncType, into map[string]bool) map[string]bool {
	if sig == nil {
		return into
	}
	for _, list := range []*ast.FieldList{sig.Params, sig.Results} {
		if list == nil {
			continue
		}
		for _, field := range list.List {
			if !isBuilderType(field.Type) {
				continue
			}
			for _, name := range field.Names {
				if into == nil {
					into = map[string]bool{}
				}
				into[name.Name] = true
			}
		}
	}
	return into
}

// isBuilderType reports whether typ names strings.Builder or bytes.Buffer,
// through any number of pointers. The package qualifier is read from the source
// as written: this pass resolves no imports, so a dot-import or a renamed one is
// not recognised, and that is a silence rather than a wrong claim.
func isBuilderType(typ ast.Expr) bool {
	for {
		star, ok := ast.Unparen(typ).(*ast.StarExpr)
		if !ok {
			break
		}
		typ = star.X
	}
	sel, ok := ast.Unparen(typ).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return false
	}
	return (pkg.Name == "strings" && sel.Sel.Name == "Builder") ||
		(pkg.Name == "bytes" && sel.Sel.Name == "Buffer")
}

// isBuilderValue reports whether e constructs a builder: a composite literal of
// builder type, its address, or `new(strings.Builder)`.
func isBuilderValue(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return isBuilderValue(x.X)
		}
	case *ast.CompositeLit:
		return isBuilderType(x.Type)
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
			return isBuilderType(x.Args[0])
		}
	}
	return false
}
