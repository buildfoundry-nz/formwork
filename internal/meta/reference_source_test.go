// reference_source_test.go — what docs/reference.md must contain, derived from
// the engine rather than from memory.
//
// #332. The completeness gate next door asserted that a registered type's NAME
// appeared somewhere in the manual and nothing more, so the manual could lose
// every parameter description and every worked example and stay green:
// replacing the whole Go/Dart/SQL region with bare backticked names left
// `go test ./internal/meta -count=1` at ok. The document tells the reader the
// opposite — "fails if a registered type or preprocessor has no section in
// this file, so this document cannot fall behind the engine silently" — so the
// gate is the half that has to change.
//
// Two derivations live here, both of them from the engine:
//
//	registeredParams — every registered type's strictly-decoded parameter set,
//	  read out of the yaml struct tags of the struct its factory decodes into.
//	  Rename or remove a param and the entry that documents it goes red.
//	manualEntries — the manual sliced into per-type entries, so an assertion
//	  lands on the entry that must carry the fact, and cannot be satisfied by
//	  some unrelated section that happens to use the same word.
package meta_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// ---------------------------------------------------------------------------
// The parameter set, read out of the rule packages' source.

// paramSet is the outcome of resolving one registered type's parameters.
// resolved distinguishes "this type takes no parameters" (sql/parses decodes
// into an anonymous empty struct) from "the extractor could not find the
// struct" — without that distinction a factory refactor would empty this gate
// silently, which is the failure this whole file exists to stop.
type paramSet struct {
	resolved bool
	names    []string
}

// rulePkg is one rule package, indexed by what the resolution walk needs:
// factory functions, named struct types, and the type names registered here.
type rulePkg struct {
	funcs map[string]*ast.FuncDecl
	types map[string]*ast.StructType
	regs  map[string]string // registered rule type -> factory func name
}

func registeredParams(t *testing.T) map[string]paramSet {
	t.Helper()
	root := filepath.Join("..", "..", "internal", "rules")
	out := map[string]paramSet{}
	fset := token.NewFileSet()
	for _, dir := range goDirs(t, root) {
		pkg := parseRulePkg(t, fset, dir)
		for typeName, factory := range pkg.regs {
			st, ok := paramStruct(pkg, factory)
			ps := paramSet{resolved: ok}
			if ok {
				ps.names = yamlParamNames(pkg, st)
			}
			out[typeName] = ps
		}
	}
	return out
}

// goDirs lists root and every directory beneath it, deepest order irrelevant.
func goDirs(t *testing.T, root string) []string {
	t.Helper()
	dirs := []string{root}
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("the rule packages are the registry's source of truth and must be readable: %v", err)
	}
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, goDirs(t, filepath.Join(root, e.Name()))...)
		}
	}
	return dirs
}

func parseRulePkg(t *testing.T, fset *token.FileSet, dir string) *rulePkg {
	t.Helper()
	pkg := &rulePkg{
		funcs: map[string]*ast.FuncDecl{},
		types: map[string]*ast.StructType{},
		regs:  map[string]string{},
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read rule package %s: %v", dir, err)
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("cannot parse %s: %v", filepath.Join(dir, name), err)
		}
		collectDecls(pkg, f)
		collectRegistrations(pkg, f)
	}
	return pkg
}

func collectDecls(pkg *rulePkg, f *ast.File) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				pkg.funcs[d.Name.Name] = d
			}
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, s := range d.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					pkg.types[ts.Name.Name] = st
				}
			}
		}
	}
}

// collectRegistrations records every rules.Register("<name>", <factory>) call.
// This is the same registry the binary builds at init, read statically, so the
// two cannot disagree without TestReferenceParamsResolveForEveryRegisteredType
// saying so.
func collectRegistrations(pkg *rulePkg, f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isPkgCall(call, "rules", "Register") || len(call.Args) != 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		fn, ok := call.Args[1].(*ast.Ident)
		if !ok {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		pkg.regs[name] = fn.Name
		return true
	})
}

// isDecodeInto matches <node>.Decode(&p) — yaml.Node's own decoder, reached
// without rules.DecodeParams and therefore without KnownFields.
func isDecodeInto(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Decode" && len(call.Args) == 1
}

func isPkgCall(call *ast.CallExpr, pkgName, funcName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != funcName {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkgName
}

// paramStruct finds the struct a factory decodes its params into: the
// destination of rules.DecodeParams (the strict path every type but one takes)
// or of a bare node.Decode, then that variable's declared type in the same
// function.
//
// dart/gate-reads-are-listened uses the second shape — node.Decode(&p), with
// no KnownFields — so its params are NOT strictly decoded and an unknown one
// is silently ignored rather than exit 2. That is a defect in
// internal/rules/dartscan, not here; this walk recognises the shape so the
// manual's parameter assertions cover all 26 types rather than 25 while it
// stands.
func paramStruct(pkg *rulePkg, factory string) (*ast.StructType, bool) {
	fn, ok := pkg.funcs[factory]
	if !ok || fn.Body == nil {
		return nil, false
	}
	var target string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var arg ast.Expr
		switch {
		case isPkgCall(call, "rules", "DecodeParams") && len(call.Args) == 2:
			arg = call.Args[1]
		case isDecodeInto(call):
			arg = call.Args[0]
		default:
			return true
		}
		u, ok := arg.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		if id, ok := u.X.(*ast.Ident); ok && target == "" {
			target = id.Name
		}
		return true
	})
	if target == "" {
		return nil, false
	}
	var st *ast.StructType
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || vs.Type == nil {
			return true
		}
		for _, nm := range vs.Names {
			if nm.Name != target {
				continue
			}
			switch typ := vs.Type.(type) {
			case *ast.Ident:
				if named, ok := pkg.types[typ.Name]; ok {
					st = named
				}
			case *ast.StructType:
				st = typ
			}
		}
		return true
	})
	return st, st != nil
}

// yamlParamNames flattens the yaml tag names of a params struct, descending
// into nested params structs declared in the same package. Nesting is a
// spelling detail of the Go type, not of the configuration surface: an
// operator writing `when: {paths_changed: [...]}` has to be told about
// paths_changed, so it counts as a parameter of the type.
func yamlParamNames(pkg *rulePkg, st *ast.StructType) []string {
	set := map[string]bool{}
	seen := map[*ast.StructType]bool{}
	var walk func(*ast.StructType)
	walk = func(s *ast.StructType) {
		if s == nil || s.Fields == nil || seen[s] {
			return
		}
		seen[s] = true
		for _, f := range s.Fields.List {
			if f.Tag != nil {
				if tag, err := strconv.Unquote(f.Tag.Value); err == nil {
					if name := yamlKey(reflect.StructTag(tag).Get("yaml")); name != "" {
						set[name] = true
					}
				}
			}
			walk(structOf(pkg, f.Type))
		}
	}
	walk(st)
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// yamlKey is the field name half of a yaml tag: "cap,omitempty" -> "cap",
// "-" -> "" (not part of the surface).
func yamlKey(tag string) string {
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	return name
}

// structOf unwraps pointers, slices and map values to a struct declared in the
// same package; anything else (a stdlib type, a regexp) has no params of ours.
func structOf(pkg *rulePkg, expr ast.Expr) *ast.StructType {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return structOf(pkg, e.X)
	case *ast.ArrayType:
		return structOf(pkg, e.Elt)
	case *ast.MapType:
		return structOf(pkg, e.Value)
	case *ast.StructType:
		return e
	case *ast.Ident:
		return pkg.types[e.Name]
	}
	return nil
}

// The extractor is the gate's evidence, so its own coverage is asserted first:
// a registered type whose params could not be resolved would otherwise make
// every parameter assertion below it pass over an empty set.
func TestReferenceParamsResolveForEveryRegisteredType(t *testing.T) {
	params := registeredParams(t)
	names := rules.TypeNames()
	if len(names) < minRegisteredTypes {
		t.Fatalf("only %d rule types registered (want >= %d) — a blank import is missing", len(names), minRegisteredTypes)
	}
	for _, n := range names {
		ps, ok := params[n]
		if !ok {
			t.Errorf("no rules.Register(%q, …) call found under internal/rules — the static "+
				"registry walk disagrees with the linked binary, and every parameter "+
				"assertion for %s would pass over an empty set", n, n)
			continue
		}
		if !ps.resolved {
			t.Errorf("could not resolve the params struct %s decodes into — the factory shape "+
				"changed and this extractor did not; fix the walk rather than letting the "+
				"manual's parameter assertions go vacuous", n)
		}
	}
}

// ---------------------------------------------------------------------------
// The manual, sliced into per-type entries.

var (
	// A type's entry is introduced either by a heading (the declarative core)
	// or by a bold bullet (the analyzer families). Both spellings anchor at the
	// start of a line, so a mention in prose is not an entry.
	headingAnchorRe = regexp.MustCompile("^#{2,6} `([^`]+)`\\s*$")
	bulletAnchorRe  = regexp.MustCompile("^- \\*\\*`([^`]+)`\\*\\*")
	anyHeadingRe    = regexp.MustCompile("^#{1,6} ")
)

// manualEntries slices the manual into the text belonging to each entry: from
// its anchor line to the next anchor, heading, section blockquote or rule.
// Fenced blocks are opaque, so a YAML comment inside an example cannot be
// mistaken for a heading and truncate the entry it lives in.
func manualEntries(manual string) map[string]string {
	entries := map[string][]string{}
	cur := ""
	inFence := false
	for _, ln := range strings.Split(manual, "\n") {
		if strings.HasPrefix(ln, "```") {
			inFence = !inFence
			if cur != "" {
				entries[cur] = append(entries[cur], ln)
			}
			continue
		}
		if !inFence {
			if m := headingAnchorRe.FindStringSubmatch(ln); m != nil {
				cur = m[1]
				entries[cur] = []string{ln}
				continue
			}
			if m := bulletAnchorRe.FindStringSubmatch(ln); m != nil {
				cur = m[1]
				entries[cur] = []string{ln}
				continue
			}
			if anyHeadingRe.MatchString(ln) || strings.HasPrefix(ln, "> ") || ln == "---" {
				cur = ""
				continue
			}
		}
		if cur != "" {
			entries[cur] = append(entries[cur], ln)
		}
	}
	out := make(map[string]string, len(entries))
	for name, lines := range entries {
		out[name] = strings.Join(lines, "\n")
	}
	return out
}

// manualSection returns the text under a heading, up to the next heading of
// the same level or shallower. Section-scoped assertions cannot be satisfied by
// some other part of a 900-line file that happens to use the words.
func manualSection(t *testing.T, manual, heading string) string {
	t.Helper()
	i := strings.Index(manual, heading+"\n")
	if i < 0 {
		t.Fatalf("docs/reference.md has no %q section", heading)
	}
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	rest := manual[i+len(heading):]
	var out []string
	for _, ln := range strings.Split(rest, "\n") {
		if h := len(ln) - len(strings.TrimLeft(ln, "#")); h > 0 && h <= level && strings.HasPrefix(ln, "#") {
			break
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// fencedBlocks returns the bodies of the ```yaml blocks inside an entry. The
// info string is required: an untagged fence is prose formatting (the
// exit-code table's shapes, a shell transcript), not a configuration example.
func fencedBlocks(entry, info string) []string {
	var out []string
	var cur []string
	open := false
	for _, ln := range strings.Split(entry, "\n") {
		if strings.HasPrefix(ln, "```") {
			if open {
				out = append(out, strings.Join(cur, "\n"))
				cur, open = nil, false
				continue
			}
			open = strings.TrimSpace(strings.TrimPrefix(ln, "```")) == info
			continue
		}
		if open {
			cur = append(cur, ln)
		}
	}
	return out
}
