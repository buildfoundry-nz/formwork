package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
	"strings"
)

// anchorParam names the param each covered type anchors its subject on.
//
// Only the two types the engine leaves unprobed. go/call-order-in-func,
// go/per-func-count-relation and dart/method-delegates carry an AnchorProbe
// already, and a second report on the same defect is duplicate noise.
var anchorParam = map[string]string{
	"go/call-confined-to-func-name": "symbol",
	"go/guard-precedes-call":        "sink",
}

// gateTreePrefixes are the trees whose rules are legitimately about gates.
var gateTreePrefixes = []string{"scripts/", "tools/", ".formwork/"}

// probeIdent is a stand-in func name used to tell "admits nothing" from
// "admits everything": a ban-idiom allowed_func matches the empty string and
// no real identifier, while `.*` matches both.
const probeIdent = "SomeFuncName"

func gateTreeScoped(includes []string) bool {
	if len(includes) == 0 {
		return false
	}
	for _, g := range includes {
		inGate := false
		for _, p := range gateTreePrefixes {
			if strings.HasPrefix(g, p) {
				inGate = true
				break
			}
		}
		if !inGate {
			return false
		}
	}
	return true
}

func banIdiom(typ string, params *yaml.Node) bool {
	if typ != "go/call-confined-to-func-name" {
		return false
	}
	allowed := mappingScalar(params, "allowed_func")
	if allowed == "" {
		return false
	}
	re, err := regexp.Compile(allowed)
	if err != nil {
		return false
	}
	// Admits the empty name and no real one: the arm's compliant end state is
	// the symbol matching nothing, so a blind anchor is its success condition.
	return re.MatchString("") && !re.MatchString(probeIdent)
}

// globRe converts a formwork scope glob to a regexp over slash paths.
func globRe(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString(`\A`)
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString(`(?:[^/]+/)*`)
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(`.*`)
			i++
		case glob[i] == '*':
			b.WriteString(`[^/]*`)
		case glob[i] == '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	b.WriteString(`\z`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile(`\A\z`)
	}
	return re
}

func matchGlob(glob, p string) bool { return globRe(glob).MatchString(p) }

func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if matchGlob(g, p) {
			return true
		}
	}
	return false
}

// importBindings mirrors the engine's: alias -> import path base.
type importBindings struct {
	aliasToBase map[string]string
	dotBases    []string
}

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
		case imp.Name != nil:
			ib.aliasToBase[imp.Name.Name] = base
		default:
			ib.aliasToBase[base] = base
		}
	}
	return ib
}

// renderSelectors returns every spelling of a call's Fun that an anchor might
// reasonably be written against: the engine's canonical alias-resolved form,
// and the raw source spelling.
//
// BOTH, deliberately. The engine resolves an alias to the import path base, so
// `workflowdata "…/workflow/data"` renders as `data.X`; a rule whose author
// wrote the anchor against the alias would then read as blind. Emitting both
// biases every ambiguous case toward LIVE, which is the safe direction: a
// missed blind anchor costs one uncaught rule, a false blind verdict condemns
// a working one and teaches readers to dismiss the census.
func renderSelectors(fun ast.Expr, ib importBindings) []string {
	canon := renderCanonical(fun, ib)
	raw := renderRaw(fun)
	out := make([]string, 0, 2)
	if canon != "" {
		out = append(out, canon)
	}
	if raw != "" && raw != canon {
		out = append(out, raw)
	}
	return out
}

func renderCanonical(fun ast.Expr, ib importBindings) string {
	switch e := fun.(type) {
	case *ast.Ident:
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
		x := renderCanonical(e.X, ib)
		if x == "" {
			return e.Sel.Name
		}
		return x + "." + e.Sel.Name
	case *ast.ParenExpr:
		return renderCanonical(e.X, ib)
	default:
		return ""
	}
}

func renderRaw(fun ast.Expr) string {
	switch e := fun.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		x := renderRaw(e.X)
		if x == "" {
			return e.Sel.Name
		}
		return x + "." + e.Sel.Name
	case *ast.ParenExpr:
		return renderRaw(e.X)
	default:
		return ""
	}
}

func callSelectors(root string, includes, excludes []string) (map[string]bool, error) {
	out := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// The engine's scanner drops these before any rule runs; a census
			// that walked them would resolve anchors against trees no rule
			// can see.
			switch d.Name() {
			case ".git", ".formwork", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel := filepath.ToSlash(mustRel(root, p))
		if !matchAny(includes, rel) || matchAny(excludes, rel) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, src, parser.AllErrors)
		if perr != nil || file == nil {
			// An unparseable file yields no selectors rather than an error:
			// a syntax error somewhere in the tree must not be able to turn
			// every anchor in the corpus into a blind-subject finding.
			return nil
		}
		ib := buildImportBindings(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, s := range renderSelectors(call.Fun, ib) {
				out[s] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func anchorLive(pattern string, selectors map[string]bool) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	for s := range selectors {
		if re.MatchString(s) {
			return true, nil
		}
	}
	return false, nil
}
