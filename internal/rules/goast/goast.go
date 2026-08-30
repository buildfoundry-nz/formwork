// Package goast implements the built-in Go-AST analyzer rule types (spec §4,
// phase 4). Each type parses a .go file with go/parser into an AST and reasons
// over the function declarations it contains. Files that are not Go source (by
// extension) yield no findings; a .go file that fails to parse is a returned
// error (the engine turns it into exit 2), never a silent pass.
package goast

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// callInfo is one call expression inside a function body, in source order.
//
// selector is the canonical package-base form after import resolution
// (qs.Redeem → quoteshare.Redeem; bare Redeem under a dot-import of
// quoteshare → quoteshare.Redeem; a local func-value alias of the same →
// quoteshare.Redeem). Without resolution, a security count-relation that
// keys on `quoteshare\.Redeem` is spelling-convention, not a gate.
//
// resultUsed is false when the call is a bare expression statement or its
// results are all assigned to `_`. A rule that counts a gate call cannot tell
// a discarded `RequireEnabled(...)` from an enforced one without this bit.
type callInfo struct {
	selector   string    // canonical rendered selector, e.g. "quoteshare.Redeem"
	line       int       // 1-based source line of the call
	pos        token.Pos // token position, used to order calls deterministically
	resultUsed bool      // false when every result is discarded
}

// funcInfo is one top-level *ast.FuncDecl with a body.
type funcInfo struct {
	name     string
	declLine int        // 1-based line of the func declaration
	bodySpan int        // Rbrace line - Lbrace line of the body block
	calls    []callInfo // calls lexically inside the body, source-ordered
}

// importBindings maps a file's import local names onto package path bases
// (the last path segment Go uses as the default name) and records which
// packages are dot-imported. Built once per file; shared by every func.
type importBindings struct {
	// localName → package path base. Covers default imports (quoteshare),
	// aliases (qs), and excludes blank imports.
	aliasToBase map[string]string
	// package path bases brought in with `import . "…"`.
	dotBases []string
}

// parseGo parses f as Go source. The bool is false (with no error) when f is
// not a .go file — callers return no findings for it. A parse failure is
// returned as an error so the engine can surface it as a config/engine error.
func parseGo(f *scan.File) (*token.FileSet, *ast.File, bool, error) {
	if !strings.HasSuffix(f.Path(), ".go") {
		return nil, nil, false, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, nil, false, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f.Path(), content, parser.AllErrors)
	if err != nil {
		return nil, nil, false, fmt.Errorf("goast: parse %s: %w", f.Path(), err)
	}
	return fset, file, true, nil
}

// buildImportBindings reads file.Imports into an alias→base map and a list of
// dot-imported package bases. The path base is path.Base of the import path
// (Go's default local name), so `…/internal/quoteshare` binds as "quoteshare"
// regardless of the full module path.
func buildImportBindings(file *ast.File) importBindings {
	ib := importBindings{aliasToBase: make(map[string]string, len(file.Imports))}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		base := path.Base(p)
		if base == "" || base == "." || base == "/" {
			continue
		}
		switch {
		case imp.Name != nil && imp.Name.Name == ".":
			ib.dotBases = append(ib.dotBases, base)
		case imp.Name != nil && imp.Name.Name == "_":
			// blank import: no call site can name it
		case imp.Name != nil:
			ib.aliasToBase[imp.Name.Name] = base
		default:
			ib.aliasToBase[base] = base
		}
	}
	return ib
}

// renderSelector renders a call's Fun expression to a flat selector string,
// resolving import aliases and a single-package dot-import to the package path
// base, and expanding local func-value aliases collected for the enclosing
// func. Non-identifier receivers collapse to the trailing selector name;
// unrenderable Fun expressions yield "".
func renderSelector(fun ast.Expr, ib importBindings, localFuncs map[string]string) string {
	switch e := fun.(type) {
	case *ast.Ident:
		if can, ok := localFuncs[e.Name]; ok && can != "" {
			return can
		}
		// Bare call under a single dot-import: treat as pkgBase.Name. Multiple
		// simultaneous package-level dot-imports of distinct packages that
		// both export the same name cannot compile, so one base is enough;
		// when several bases are present we leave the bare name (no worse
		// than pre-resolution) rather than guess.
		if len(ib.dotBases) == 1 {
			return ib.dotBases[0] + "." + e.Name
		}
		return e.Name
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if base, ok := ib.aliasToBase[id.Name]; ok {
				return base + "." + e.Sel.Name
			}
		}
		x := renderSelector(e.X, ib, localFuncs)
		if x == "" {
			return e.Sel.Name
		}
		return x + "." + e.Sel.Name
	case *ast.ParenExpr:
		return renderSelector(e.X, ib, localFuncs)
	default:
		return ""
	}
}

// collectFuncValues walks body once and records local names bound to a
// package selector (or to another already-resolved local name) as a func
// value — `redeemFn := quoteshare.Redeem`, `var f = qs.Redeem`, `f = Redeem`
// under a dot-import. Call sites through those names then render as the
// underlying canonical selector. Only same-func, direct bindings: no
// cross-func flow, no returns of func values (documented residual).
func collectFuncValues(body *ast.BlockStmt, ib importBindings) map[string]string {
	out := make(map[string]string)
	// Seed with nothing; resolve RHS against ib and against out as we go so a
	// chain `a := pkg.F; b := a` still resolves. Walk statements in source
	// order via Inspect's depth-first, but only record AssignStmt / ValueSpec
	// bindings that are not calls.
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, lhs := range s.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				// Skip call RHS — that's a result binding, not a func value.
				if _, isCall := s.Rhs[i].(*ast.CallExpr); isCall {
					continue
				}
				if can := renderSelector(s.Rhs[i], ib, out); can != "" && strings.Contains(can, ".") {
					out[id.Name] = can
				}
			}
		case *ast.ValueSpec:
			for i, name := range s.Names {
				if name.Name == "_" || i >= len(s.Values) {
					continue
				}
				if _, isCall := s.Values[i].(*ast.CallExpr); isCall {
					continue
				}
				if can := renderSelector(s.Values[i], ib, out); can != "" && strings.Contains(can, ".") {
					out[name.Name] = can
				}
			}
		}
		return true
	})
	return out
}

// resultIsUsed reports whether a CallExpr's results are consumed. False when
// the call is a bare ExprStmt (`pkg.F()`) or every LHS of its AssignStmt is
// `_`. Any other parent (if-init, return, value use, named assign) counts as
// used. Without types we cannot see a later-unused binding; that residual is
// the same class errcheck owns.
func resultIsUsed(parent ast.Node) bool {
	switch p := parent.(type) {
	case *ast.ExprStmt:
		return false
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != "_" {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// extractFuncs returns the top-level functions with bodies in file, each with
// its calls in source order. Calls inside nested function literals are
// attributed to the enclosing top-level func (heuristic limitation).
//
// Import aliases, a single-package dot-import, and same-func func-value
// aliases are resolved into the package-base form before a call is recorded,
// so a rule matching `quoteshare\.Redeem` sees `qs.Redeem` and a bare
// `Redeem` under `import . "…/quoteshare"` the same way. Each call also
// records whether its results are used, so a count-relation can refuse to
// count a discarded gate call as enforcement.
func extractFuncs(fset *token.FileSet, file *ast.File) []funcInfo {
	ib := buildImportBindings(file)
	var out []funcInfo
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fi := funcInfo{
			name:     fd.Name.Name,
			declLine: fset.Position(fd.Pos()).Line,
			bodySpan: fset.Position(fd.Body.Rbrace).Line - fset.Position(fd.Body.Lbrace).Line,
		}
		localFuncs := collectFuncValues(fd.Body, ib)
		// Parent stack so we can tell a bare ExprStmt call from a used one.
		var stack []ast.Node
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if n == nil {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				return false
			}
			if ce, ok := n.(*ast.CallExpr); ok {
				var parent ast.Node
				if len(stack) > 0 {
					parent = stack[len(stack)-1]
				}
				fi.calls = append(fi.calls, callInfo{
					selector:   renderSelector(ce.Fun, ib, localFuncs),
					line:       fset.Position(ce.Pos()).Line,
					pos:        ce.Pos(),
					resultUsed: resultIsUsed(parent),
				})
			}
			stack = append(stack, n)
			return true
		})
		sort.Slice(fi.calls, func(i, j int) bool { return fi.calls[i].pos < fi.calls[j].pos })
		out = append(out, fi)
	}
	return out
}

// compileRe compiles a required regex param, wrapping failures with rule/field
// context so a bad regex reads as a config error.
func compileRe(rule, field, pat string) (*regexp.Regexp, error) {
	if pat == "" {
		return nil, fmt.Errorf("%s: params.%s is required", rule, field)
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid %s: %w", rule, field, err)
	}
	return re, nil
}

// compileOptRe compiles an optional regex param; an empty value returns a nil
// matcher meaning "match all".
func compileOptRe(rule, field, pat string) (*regexp.Regexp, error) {
	if pat == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid %s: %w", rule, field, err)
	}
	return re, nil
}

// matchesFuncFilter reports whether a func of the given name is in scope for an
// optional funcs filter (a nil filter matches every func).
func matchesFuncFilter(filter *regexp.Regexp, name string) bool {
	return filter == nil || filter.MatchString(name)
}
